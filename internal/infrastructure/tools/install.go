package tools

import (
	"archive/tar"
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ulikunitz/xz"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

const (
	// maxDownloadBytes membatasi ukuran unduhan; build FFmpeg terbesar
	// berada jauh di bawah angka ini.
	maxDownloadBytes = 300 << 20
	downloadTimeout  = 15 * time.Minute
)

// Install mengunduh, memverifikasi, dan memasang satu tool.
//
// Urutannya sengaja: verifikasi checksum terjadi sebelum satu byte pun
// diekstrak, sehingga arsip yang tidak cocok tidak pernah disentuh lebih jauh.
func (m *Manager) Install(ctx context.Context, name string) error {
	if name == FFprobe {
		name = FFmpeg // keduanya berasal dari arsip yang sama
	}

	build, ok := m.manifest.BuildFor(name)
	if !ok {
		return domain.NewError(domain.CodeToolManifest, domain.ClassLocal,
			fmt.Sprintf("%s tidak punya entri build untuk %s", name, platformKey()))
	}
	if !build.Installable() {
		detail := fmt.Sprintf("%s/%s belum di-pin di manifest", name, platformKey())
		if build.Note != "" {
			detail += ": " + build.Note
		}
		return domain.NewError(domain.CodeToolManifest, domain.ClassLocal, detail)
	}

	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"buat direktori tools", err)
	}

	// Seluruh unduhan diverifikasi sebelum satu pun diekstrak. Build yang
	// terdiri dari beberapa arsip, seperti ffmpeg dan ffprobe yang diterbitkan
	// terpisah, tidak boleh terpasang separuh karena arsip keduanya ternyata
	// tidak cocok dengan manifest.
	var archives []string
	defer func() {
		for _, p := range archives {
			_ = os.Remove(p)
		}
	}()

	steps := len(build.Downloads)
	defer m.clearProgress(name)

	for i, d := range build.Downloads {
		step := i + 1
		m.log.Info("mengunduh tool", "tool", name, "url", d.URL)
		m.setProgress(name, application.ToolProgress{
			Phase: application.ToolPhaseDownloading, Step: step, Steps: steps,
		})
		p, err := m.download(ctx, d, func(done, total int64) {
			m.setProgress(name, application.ToolProgress{
				Phase: application.ToolPhaseDownloading, Step: step, Steps: steps,
				DoneBytes: done, TotalBytes: total,
			})
		})
		if err != nil {
			return err
		}
		archives = append(archives, p)
	}

	m.setProgress(name, application.ToolProgress{
		Phase: application.ToolPhaseExtracting, Step: steps, Steps: steps,
	})

	for i, d := range build.Downloads {
		if err := m.extract(archives[i], d); err != nil {
			return err
		}
	}

	m.Invalidate()
	m.log.Info("tool terpasang", "tool", name, "dir", m.dir)
	return nil
}

// download mengambil berkas ke temp sambil menghitung SHA-256, lalu
// membandingkannya dengan manifest.
//
// onBytes dipanggil setiap potongan tertulis dengan jumlah byte sejauh ini
// dan ukuran total dari Content-Length, atau 0 bila server tidak
// menyebutkannya.
func (m *Manager) download(ctx context.Context, build Download, onBytes func(done, total int64)) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, build.URL, nil)
	if err != nil {
		return "", domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"susun request", err)
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return "", domain.WrapError(domain.CodeToolInstallFailed, domain.ClassTransient,
			"unduh tool", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", domain.NewError(domain.CodeToolInstallFailed, domain.ClassTransient,
			fmt.Sprintf("unduhan menjawab %s", resp.Status))
	}

	if err := os.MkdirAll(m.tmpDir, 0o755); err != nil {
		return "", domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"buat direktori temp", err)
	}

	tmp, err := os.CreateTemp(m.tmpDir, "tool-*.download")
	if err != nil {
		return "", domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"buat berkas temp", err)
	}
	tmpName := tmp.Name()

	hasher := sha256.New()
	limited := io.LimitReader(resp.Body, maxDownloadBytes+1)
	counter := &byteCounter{total: max(resp.ContentLength, 0), report: onBytes}
	written, copyErr := io.Copy(io.MultiWriter(tmp, hasher, counter), limited)
	closeErr := tmp.Close()

	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		return "", domain.WrapError(domain.CodeToolInstallFailed, domain.ClassTransient,
			"tulis unduhan", errors.Join(copyErr, closeErr))
	}
	if written > maxDownloadBytes {
		_ = os.Remove(tmpName)
		return "", domain.NewError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"unduhan melebihi batas ukuran")
	}

	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, build.SHA256) {
		_ = os.Remove(tmpName)
		return "", domain.NewError(domain.CodeChecksumMismatch, domain.ClassPermanent,
			fmt.Sprintf("checksum tidak cocok: dapat %s, manifest %s", got, build.SHA256))
	}
	return tmpName, nil
}

// extract mengambil hanya berkas yang terdaftar di build.Extract.
//
// Pencocokan memakai basename dan tujuan penulisan selalu direktori tools
// milik kita, sehingga path traversal dari dalam arsip tidak mungkin terjadi.
func (m *Manager) extract(archivePath string, build Download) error {
	switch build.Archive {
	case ArchiveNone:
		if len(build.Extract) != 1 {
			return domain.NewError(domain.CodeToolInstallFailed, domain.ClassLocal,
				"archive none harus punya tepat satu entri extract")
		}
		return m.installFile(archivePath, normalizeTarget(build.Extract[0]))
	case ArchiveZip:
		return m.extractZip(archivePath, build.Extract)
	case ArchiveTarXZ:
		return m.extractTarXZ(archivePath, build.Extract)
	default:
		return domain.NewError(domain.CodeToolInstallFailed, domain.ClassLocal,
			fmt.Sprintf("bentuk arsip %q tidak dikenal", build.Archive))
	}
}

// normalizeTarget menyeragamkan nama tujuan mengikuti konvensi OS.
func normalizeTarget(name string) string {
	return exeName(strings.TrimSuffix(name, ".exe"))
}

// wanted mencocokkan basename sebuah entri arsip dengan daftar yang dicari.
func wanted(entryPath string, list []string) (string, bool) {
	base := path.Base(filepath.ToSlash(entryPath))
	for _, w := range list {
		if strings.EqualFold(base, w) {
			return normalizeTarget(w), true
		}
	}
	return "", false
}

func (m *Manager) extractZip(archivePath string, list []string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal, "buka zip", err)
	}
	defer func() { _ = r.Close() }()

	found := 0
	for _, f := range r.File {
		target, ok := wanted(f.Name, list)
		if !ok || f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
				"baca entri zip", err)
		}
		err = m.writeTool(rc, target)
		_ = rc.Close()
		if err != nil {
			return err
		}
		found++
	}
	return checkFound(found, list)
}

func (m *Manager) extractTarXZ(archivePath string, list []string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal, "buka arsip", err)
	}
	defer func() { _ = f.Close() }()

	xzr, err := xz.NewReader(f)
	if err != nil {
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal, "buka xz", err)
	}

	tr := tar.NewReader(xzr)
	found := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
				"baca tar", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		target, ok := wanted(hdr.Name, list)
		if !ok {
			continue
		}
		if err := m.writeTool(tr, target); err != nil {
			return err
		}
		found++
	}
	return checkFound(found, list)
}

// checkFound memastikan seluruh berkas yang dicari benar-benar ada.
func checkFound(found int, list []string) error {
	if found < len(list) {
		return domain.NewError(domain.CodeToolInstallFailed, domain.ClassLocal,
			fmt.Sprintf("hanya %d dari %d berkas ditemukan di arsip", found, len(list)))
	}
	return nil
}

// installFile menyalin berkas tunggal (arsip "none") ke direktori tools.
func (m *Manager) installFile(src, target string) error {
	f, err := os.Open(src)
	if err != nil {
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"buka unduhan", err)
	}
	defer func() { _ = f.Close() }()
	return m.writeTool(f, target)
}

// writeTool menulis satu binary ke direktori tools lewat rename atomik.
func (m *Manager) writeTool(r io.Reader, target string) error {
	tmp, err := os.CreateTemp(m.dir, target+".*.tmp")
	if err != nil {
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"buat temp tool", err)
	}
	tmpName := tmp.Name()

	_, copyErr := io.Copy(tmp, io.LimitReader(r, maxDownloadBytes))
	chmodErr := tmp.Chmod(0o755)
	closeErr := tmp.Close()

	if err := errors.Join(copyErr, chmodErr, closeErr); err != nil {
		_ = os.Remove(tmpName)
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"tulis tool", err)
	}

	// Rename menimpa berkas lama secara atomik, termasuk di Windows (Go
	// memakai MoveFileEx dengan MOVEFILE_REPLACE_EXISTING). Menghapus
	// tujuan lebih dulu justru menciptakan jeda tanpa tool sama sekali bila
	// proses mati di antara keduanya.
	final := filepath.Join(m.dir, target)
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"pasang tool", err)
	}
	return nil
}

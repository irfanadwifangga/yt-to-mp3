// Package logging menulis log aplikasi ke berkas harian beserta terminal.
package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// currentName adalah berkas yang sedang ditulisi. Nama tetap membuat
	// "buka log terbaru" selalu menunjuk berkas yang sama.
	currentName = "app.log"

	archivePrefix = "app-"
	archiveSuffix = ".log"
	dateLayout    = "2006-01-02"
)

// DailyFile adalah io.Writer yang merotasi berkas log sekali sehari dan
// membuang arsip yang melewati masa simpan.
//
// Rotasi dikerjakan sendiri alih-alih memakai pustaka: kebutuhannya kecil,
// dan setiap dependensi tambahan wajib pure Go (ADR-011).
type DailyFile struct {
	dir    string
	retain int
	now    func() time.Time

	mu   sync.Mutex
	file *os.File
	day  string // tanggal isi berkas yang sedang terbuka
}

// OpenDaily membuka app.log di dir dan menyimpan retain arsip harian.
//
// Berkas dari hari sebelumnya langsung diarsipkan saat dibuka, sehingga
// aplikasi yang dibuka sekali seminggu tidak menumpuk seminggu log dalam
// satu berkas bertanggal hari pertama.
func OpenDaily(dir string, retain int, now func() time.Time) (*DailyFile, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("buat direktori log: %w", err)
	}

	d := &DailyFile{dir: dir, retain: retain, now: now}
	path := filepath.Join(dir, currentName)

	if info, err := os.Stat(path); err == nil {
		d.day = info.ModTime().Format(dateLayout)
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.rotateIfNeeded(); err != nil {
		return nil, err
	}
	if d.file == nil {
		if err := d.open(); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// Write menulis satu baris log, merotasi lebih dulu bila hari berganti.
func (d *DailyFile) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.rotateIfNeeded(); err != nil {
		return 0, err
	}
	if d.file == nil {
		if err := d.open(); err != nil {
			return 0, err
		}
	}
	return d.file.Write(p)
}

// Close menutup berkas log.
func (d *DailyFile) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.file == nil {
		return nil
	}
	err := d.file.Close()
	d.file = nil
	return err
}

func (d *DailyFile) open() error {
	f, err := os.OpenFile(filepath.Join(d.dir, currentName), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("buka berkas log: %w", err)
	}
	d.file = f
	if d.day == "" {
		d.day = d.now().Format(dateLayout)
	}
	return nil
}

// rotateIfNeeded mengarsipkan app.log bila isinya milik hari lain.
func (d *DailyFile) rotateIfNeeded() error {
	today := d.now().Format(dateLayout)
	if d.day == "" || d.day == today {
		return nil
	}

	// Windows menolak me-rename berkas yang masih terbuka, jadi handle
	// ditutup lebih dulu.
	if d.file != nil {
		_ = d.file.Close()
		d.file = nil
	}

	current := filepath.Join(d.dir, currentName)
	archive := filepath.Join(d.dir, archivePrefix+d.day+archiveSuffix)
	if err := appendOrRename(current, archive); err != nil {
		return err
	}

	d.day = today
	d.prune(today)
	return nil
}

// appendOrRename memindahkan current ke archive. Bila arsip bertanggal sama
// sudah ada, misalnya karena jam sistem sempat mundur, isinya disambung
// alih-alih ditimpa.
func appendOrRename(current, archive string) error {
	if _, err := os.Stat(archive); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(current, archive); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("arsipkan log: %w", err)
		}
		return nil
	}

	src, err := os.Open(current)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("baca log lama: %w", err)
	}
	defer func() { _ = src.Close() }()

	dst, err := os.OpenFile(archive, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("buka arsip log: %w", err)
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return fmt.Errorf("sambung arsip log: %w", err)
	}
	_ = src.Close()
	return os.Remove(current)
}

// prune membuang arsip yang tanggalnya lewat masa simpan.
//
// Umur dibaca dari nama berkas, bukan waktu modifikasi: menyalin folder data
// ke komputer lain mengubah mtime tetapi tidak mengubah hari isi log.
func (d *DailyFile) prune(today string) {
	if d.retain <= 0 {
		return
	}
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return
	}

	var archives []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, archivePrefix) || !strings.HasSuffix(name, archiveSuffix) {
			continue
		}
		day := strings.TrimSuffix(strings.TrimPrefix(name, archivePrefix), archiveSuffix)
		if _, err := time.Parse(dateLayout, day); err != nil {
			continue // bukan arsip milik kita
		}
		archives = append(archives, day)
	}

	todayTime, _ := time.Parse(dateLayout, today)
	cutoff := todayTime.AddDate(0, 0, -d.retain).Format(dateLayout)

	sort.Strings(archives)
	for _, day := range archives {
		// Tanggal berformat YYYY-MM-DD dapat dibandingkan sebagai string.
		if day < cutoff {
			_ = os.Remove(filepath.Join(d.dir, archivePrefix+day+archiveSuffix))
		}
	}
}

// Tee menulis ke beberapa writer dan mengabaikan kegagalan per writer.
//
// io.MultiWriter berhenti pada error pertama. Pada build Windows tanpa
// console, stderr tidak valid dan setiap penulisan gagal, sehingga berkas
// log di belakangnya tidak pernah ditulisi. Log adalah jalur diagnosis
// terakhir; satu tujuan yang rusak tidak boleh mematikan tujuan lain.
func Tee(writers ...io.Writer) io.Writer {
	return tee(writers)
}

type tee []io.Writer

func (t tee) Write(p []byte) (int, error) {
	var ok bool
	var firstErr error
	for _, w := range t {
		if _, err := w.Write(p); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		ok = true
	}
	if !ok && firstErr != nil {
		return 0, firstErr
	}
	return len(p), nil
}

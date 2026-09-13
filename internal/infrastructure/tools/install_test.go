package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// newManager menyiapkan Manager yang mengarah ke direktori sementara dan
// server HTTP lokal, sehingga test tidak pernah menyentuh jaringan.
//
// files memetakan path URL ke isinya. URL unduhan pada build yang diawali
// "/" dilengkapi dengan alamat server test.
func newManager(t *testing.T, files map[string][]byte, build Build) *Manager {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	for i, d := range build.Downloads {
		if strings.HasPrefix(d.URL, "/") {
			build.Downloads[i].URL = srv.URL + d.URL
		}
	}

	dir := t.TempDir()
	m := &Manager{
		dir:    filepath.Join(dir, "tools"),
		tmpDir: filepath.Join(dir, "tmp"),
		log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		client: srv.Client(),
		cache:  make(map[string]cacheEntry),
		now:    time.Now,
		manifest: Manifest{
			Schema: manifestSchema,
			Tools: map[string]Tool{
				"probe-tool": {
					Version: "1.0.0",
					Builds:  map[string]Build{platformKey(): build},
				},
			},
		},
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		t.Fatalf("siapkan dir: %v", err)
	}
	return m
}

// single menyusun build satu unduhan yang dilayani di /artifact.
func single(payload []byte, d Download) (map[string][]byte, Build) {
	d.URL = "/artifact"
	return map[string][]byte{"/artifact": payload}, Build{Downloads: []Download{d}}
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("buat entri zip: %v", err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("tulis entri zip: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("tutup zip: %v", err)
	}
	return buf.Bytes()
}

// wantCode memastikan error membawa kode domain yang diharapkan.
func wantCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("mau error %s, dapat nil", code)
	}
	var derr *domain.Error
	if !errors.As(err, &derr) {
		t.Fatalf("error bukan *domain.Error: %v", err)
	}
	if derr.Code != code {
		t.Fatalf("kode = %s, mau %s (detail: %s)", derr.Code, code, derr.Detail)
	}
}

func installedCount(t *testing.T, m *Manager) int {
	t.Helper()
	entries, _ := os.ReadDir(m.dir)
	return len(entries)
}

// Manifest yang belum di-pin harus menolak instalasi. Ini penjaga utama
// terhadap menjalankan binary pihak ketiga tanpa verifikasi (ADR-033).
func TestInstallMenolakManifestBelumDipin(t *testing.T) {
	files, build := single([]byte("binary palsu"), Download{
		SHA256:  "", // belum di-pin
		Archive: ArchiveNone,
		Extract: []string{"probe-tool"},
	})
	m := newManager(t, files, build)

	err := m.Install(context.Background(), "probe-tool")
	wantCode(t, err, domain.CodeToolManifest)

	if n := installedCount(t, m); n != 0 {
		t.Errorf("tidak boleh ada berkas terpasang, ada %d", n)
	}
}

func TestInstallMenolakChecksumTidakCocok(t *testing.T) {
	files, build := single([]byte("binary palsu"), Download{
		SHA256:  sum([]byte("isi yang berbeda")),
		Archive: ArchiveNone,
		Extract: []string{"probe-tool"},
	})
	m := newManager(t, files, build)

	err := m.Install(context.Background(), "probe-tool")
	wantCode(t, err, domain.CodeChecksumMismatch)

	if n := installedCount(t, m); n != 0 {
		t.Errorf("berkas tidak boleh terpasang saat checksum gagal, ada %d", n)
	}
}

func TestInstallBerkasTunggal(t *testing.T) {
	payload := []byte("#!/bin/sh\necho halo\n")
	files, build := single(payload, Download{
		SHA256:  sum(payload),
		Archive: ArchiveNone,
		Extract: []string{"probe-tool"},
	})
	m := newManager(t, files, build)

	if err := m.Install(context.Background(), "probe-tool"); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	target := filepath.Join(m.dir, exeName("probe-tool"))
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("baca hasil: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Error("isi berkas terpasang tidak sama dengan unduhan")
	}
}

// Arsip nyata menaruh binary di dalam subdirektori bernama versi. Pencocokan
// memakai basename supaya nama folder hulu boleh berubah tanpa merusak kita,
// sekaligus membuat path traversal dari dalam arsip mustahil.
func TestInstallZipMengambilBerdasarBasename(t *testing.T) {
	payload := zipOf(t, map[string]string{
		"ffmpeg-9.0.1-essentials_build/bin/probe-tool": "isi tool",
		"ffmpeg-9.0.1-essentials_build/README.txt":     "abaikan saya",
		"../../../jahat.txt":                           "jangan pernah ditulis",
	})
	files, build := single(payload, Download{
		SHA256:  sum(payload),
		Archive: ArchiveZip,
		Extract: []string{"probe-tool"},
	})
	m := newManager(t, files, build)

	if err := m.Install(context.Background(), "probe-tool"); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	got, err := os.ReadFile(filepath.Join(m.dir, exeName("probe-tool")))
	if err != nil {
		t.Fatalf("baca hasil: %v", err)
	}
	if string(got) != "isi tool" {
		t.Errorf("isi = %q, mau %q", got, "isi tool")
	}

	if n := installedCount(t, m); n != 1 {
		t.Errorf("hanya berkas yang diminta boleh terpasang, ada %d", n)
	}
	if _, err := os.Stat(filepath.Join(m.dir, "jahat.txt")); err == nil {
		t.Error("entri path traversal ikut tertulis")
	}
}

func TestInstallGagalBilaBerkasTidakAdaDiArsip(t *testing.T) {
	payload := zipOf(t, map[string]string{"bundle/lain.txt": "bukan yang dicari"})
	files, build := single(payload, Download{
		SHA256:  sum(payload),
		Archive: ArchiveZip,
		Extract: []string{"probe-tool"},
	})
	m := newManager(t, files, build)

	err := m.Install(context.Background(), "probe-tool")
	wantCode(t, err, domain.CodeToolInstallFailed)
}

// Sebagian sumber menerbitkan ffmpeg dan ffprobe sebagai arsip terpisah.
// Keduanya harus terpasang dari satu permintaan instalasi.
func TestInstallBeberapaUnduhan(t *testing.T) {
	first := zipOf(t, map[string]string{"probe-tool": "alat pertama"})
	second := zipOf(t, map[string]string{"probe-helper": "alat kedua"})

	m := newManager(t,
		map[string][]byte{"/first.zip": first, "/second.zip": second},
		Build{Downloads: []Download{
			{URL: "/first.zip", SHA256: sum(first), Archive: ArchiveZip, Extract: []string{"probe-tool"}},
			{URL: "/second.zip", SHA256: sum(second), Archive: ArchiveZip, Extract: []string{"probe-helper"}},
		}},
	)

	if err := m.Install(context.Background(), "probe-tool"); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	for name, want := range map[string]string{"probe-tool": "alat pertama", "probe-helper": "alat kedua"} {
		got, err := os.ReadFile(filepath.Join(m.dir, exeName(name)))
		if err != nil {
			t.Fatalf("baca %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, mau %q", name, got, want)
		}
	}
}

// Checksum arsip kedua yang salah tidak boleh meninggalkan arsip pertama
// terpasang: ffmpeg tanpa ffprobe tampak siap tetapi gagal saat verifikasi
// hasil konversi.
func TestInstallBeberapaUnduhanTidakTerpasangSeparuh(t *testing.T) {
	first := zipOf(t, map[string]string{"probe-tool": "alat pertama"})
	second := zipOf(t, map[string]string{"probe-helper": "alat kedua"})

	m := newManager(t,
		map[string][]byte{"/first.zip": first, "/second.zip": second},
		Build{Downloads: []Download{
			{URL: "/first.zip", SHA256: sum(first), Archive: ArchiveZip, Extract: []string{"probe-tool"}},
			{URL: "/second.zip", SHA256: sum([]byte("lain")), Archive: ArchiveZip, Extract: []string{"probe-helper"}},
		}},
	)

	err := m.Install(context.Background(), "probe-tool")
	wantCode(t, err, domain.CodeChecksumMismatch)

	if n := installedCount(t, m); n != 0 {
		t.Errorf("tidak boleh ada berkas terpasang, ada %d", n)
	}
	if entries, _ := os.ReadDir(m.tmpDir); len(entries) != 0 {
		t.Errorf("arsip sementara tertinggal: %d berkas", len(entries))
	}
}

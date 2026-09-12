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
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// newManager menyiapkan Manager yang mengarah ke direktori sementara dan
// server HTTP lokal, sehingga test tidak pernah menyentuh jaringan.
func newManager(t *testing.T, payload []byte, build Build) *Manager {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)

	if build.URL == "" && payload != nil {
		build.URL = srv.URL + "/artifact"
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
			Schema: 1,
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

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
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

// Manifest yang belum di-pin harus menolak instalasi. Ini penjaga utama
// terhadap menjalankan binary pihak ketiga tanpa verifikasi (ADR-033).
func TestInstallMenolakManifestBelumDipin(t *testing.T) {
	payload := []byte("binary palsu")
	m := newManager(t, payload, Build{
		SHA256:  "", // belum di-pin
		Archive: ArchiveNone,
		Extract: []string{"probe-tool"},
	})

	err := m.Install(context.Background(), "probe-tool")
	wantCode(t, err, domain.CodeToolManifest)

	if entries, _ := os.ReadDir(m.dir); len(entries) != 0 {
		t.Errorf("tidak boleh ada berkas terpasang, ada %d", len(entries))
	}
}

func TestInstallMenolakChecksumTidakCocok(t *testing.T) {
	payload := []byte("binary palsu")
	m := newManager(t, payload, Build{
		SHA256:  sum([]byte("isi yang berbeda")),
		Archive: ArchiveNone,
		Extract: []string{"probe-tool"},
	})

	err := m.Install(context.Background(), "probe-tool")
	wantCode(t, err, domain.CodeChecksumMismatch)

	if entries, _ := os.ReadDir(m.dir); len(entries) != 0 {
		t.Errorf("berkas tidak boleh terpasang saat checksum gagal, ada %d", len(entries))
	}
}

func TestInstallBerkasTunggal(t *testing.T) {
	payload := []byte("#!/bin/sh\necho halo\n")
	m := newManager(t, payload, Build{
		SHA256:  sum(payload),
		Archive: ArchiveNone,
		Extract: []string{"probe-tool"},
	})

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
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"ffmpeg-n7.1-win64-gpl/bin/probe-tool": "isi tool",
		"ffmpeg-n7.1-win64-gpl/README.txt":     "abaikan saya",
		"../../../jahat.txt":                   "jangan pernah ditulis",
	}
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
	payload := buf.Bytes()

	m := newManager(t, payload, Build{
		SHA256:  sum(payload),
		Archive: ArchiveZip,
		Extract: []string{"probe-tool"},
	})

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

	entries, _ := os.ReadDir(m.dir)
	if len(entries) != 1 {
		t.Errorf("hanya berkas yang diminta boleh terpasang, ada %d", len(entries))
	}
	if _, err := os.Stat(filepath.Join(m.dir, "jahat.txt")); err == nil {
		t.Error("entri path traversal ikut tertulis")
	}
}

func TestInstallGagalBilaBerkasTidakAdaDiArsip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("bundle/lain.txt")
	_, _ = w.Write([]byte("bukan yang dicari"))
	_ = zw.Close()
	payload := buf.Bytes()

	m := newManager(t, payload, Build{
		SHA256:  sum(payload),
		Archive: ArchiveZip,
		Extract: []string{"probe-tool"},
	})

	err := m.Install(context.Background(), "probe-tool")
	wantCode(t, err, domain.CodeToolInstallFailed)
}

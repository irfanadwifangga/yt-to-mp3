package tools

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// fakeGitHub meniru API rilis GitHub dan host unduhannya.
func fakeGitHub(t *testing.T, ytdlpTag, sums string, payload []byte) *Manager {
	t.Helper()

	asset := YTDLPAssets[platformKey()].Asset
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/yt-dlp/yt-dlp/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":%q}`, ytdlpTag)
	})
	mux.HandleFunc("/repos/GyanD/codexffmpeg/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"9.1"}`))
	})
	mux.HandleFunc("/yt-dlp/yt-dlp/releases/download/"+ytdlpTag+"/SHA2-256SUMS", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sums))
	})
	mux.HandleFunc("/yt-dlp/yt-dlp/releases/download/"+ytdlpTag+"/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	return &Manager{
		dir:            filepath.Join(dir, "tools"),
		tmpDir:         filepath.Join(dir, "tmp"),
		log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		client:         srv.Client(),
		cache:          make(map[string]cacheEntry),
		now:            time.Now,
		githubAPI:      srv.URL,
		githubDownload: srv.URL,
	}
}

func skipWithoutYTDLPBuild(t *testing.T) {
	t.Helper()
	if _, ok := YTDLPAssets[platformKey()]; !ok {
		t.Skipf("yt-dlp tidak menerbitkan build untuk %s", platformKey())
	}
}

func TestLatestVersion(t *testing.T) {
	m := fakeGitHub(t, "2026.09.01", "", nil)
	ctx := context.Background()

	for name, want := range map[string]string{YTDLP: "2026.09.01", FFmpeg: "9.1", FFprobe: "9.1"} {
		got, err := m.LatestVersion(ctx, name)
		if err != nil || got != want {
			t.Errorf("LatestVersion(%s) = %q, %v; mau %q", name, got, err, want)
		}
	}
}

// Tag dari API dipakai menyusun URL unduhan, jadi bentuk yang tidak terduga
// harus ditolak sebelum menyentuh jaringan lagi.
func TestLatestVersionMenolakTagAneh(t *testing.T) {
	m := fakeGitHub(t, "../../evil", "", nil)
	if _, err := m.LatestVersion(context.Background(), YTDLP); err == nil {
		t.Error("tag berisi path seharusnya ditolak")
	}
}

func TestInstallLatestYTDLP(t *testing.T) {
	skipWithoutYTDLPBuild(t)
	payload := []byte("binary yt-dlp terbaru")
	asset := YTDLPAssets[platformKey()]
	sums := fmt.Sprintf("%s  yt-dlp.tar.gz\n%s  %s\n", sum([]byte("lain")), sum(payload), asset.Asset)

	m := fakeGitHub(t, "2026.09.01", sums, payload)
	version, err := m.InstallLatestYTDLP(context.Background())
	if err != nil {
		t.Fatalf("InstallLatestYTDLP() error = %v", err)
	}
	if version != "2026.09.01" {
		t.Errorf("versi = %q", version)
	}

	got, err := os.ReadFile(filepath.Join(m.dir, normalizeTarget(asset.Target)))
	if err != nil {
		t.Fatalf("baca hasil: %v", err)
	}
	if string(got) != string(payload) {
		t.Error("isi berkas terpasang tidak sama dengan unduhan")
	}
}

// Checksum yang diterbitkan rilis tetap wajib cocok; pembaruan tidak pernah
// melewati verifikasi.
func TestInstallLatestYTDLPMenolakChecksumSalah(t *testing.T) {
	skipWithoutYTDLPBuild(t)
	asset := YTDLPAssets[platformKey()]
	sums := fmt.Sprintf("%s  %s\n", sum([]byte("isi lain")), asset.Asset)

	m := fakeGitHub(t, "2026.09.01", sums, []byte("binary disusupi"))
	_, err := m.InstallLatestYTDLP(context.Background())
	wantCode(t, err, domain.CodeChecksumMismatch)

	if entries, _ := os.ReadDir(m.dir); len(entries) != 0 {
		t.Errorf("berkas terpasang walau checksum gagal: %d", len(entries))
	}
}

func TestInstallLatestYTDLPTanpaEntriChecksum(t *testing.T) {
	skipWithoutYTDLPBuild(t)
	m := fakeGitHub(t, "2026.09.01", "abc  aset-lain\n", []byte("x"))
	_, err := m.InstallLatestYTDLP(context.Background())
	wantCode(t, err, domain.CodeToolInstallFailed)
}

func TestUpdateStateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-updates.json")
	store := NewUpdateStateFile(path)

	empty, err := store.Load()
	if err != nil || !empty.CheckedAt.IsZero() {
		t.Fatalf("Load() berkas belum ada = %+v, %v", empty, err)
	}

	want := application.ToolUpdateState{
		CheckedAt: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC),
		Latest:    map[string]string{YTDLP: "2026.09.01"},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil || !got.CheckedAt.Equal(want.CheckedAt) || got.Latest[YTDLP] != "2026.09.01" {
		t.Errorf("Load() = %+v, %v", got, err)
	}

	if err := os.WriteFile(path, []byte("{rusak"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Load(); err != nil || !got.CheckedAt.IsZero() {
		t.Errorf("berkas rusak = %+v, %v; mau diperlakukan belum pernah dicek", got, err)
	}
}

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
	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
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
	mux.HandleFunc("/repos/"+version.Repo+"/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.0"}`))
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

	// Versi aplikasi dikembalikan tanpa awalan v, sama dengan versi yang
	// disematkan GoReleaser.
	for name, want := range map[string]string{
		YTDLP: "2026.09.01", FFmpeg: "9.1", FFprobe: "9.1", version.AppName: "1.2.0",
	} {
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

// fakeFFmpegRelease menambahkan rilis GyanD 9.1 ke server GitHub palsu:
// daftar aset dengan digest dan arsip essentials-nya.
func fakeFFmpegRelease(t *testing.T, digest string, archive []byte) *Manager {
	t.Helper()
	name := FFmpegWindowsAsset("9.1")
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/GyanD/codexffmpeg/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"9.1"}`))
	})
	mux.HandleFunc("/repos/GyanD/codexffmpeg/releases/tags/9.1", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name":"9.1","assets":[{"name":"ffmpeg-9.1-full_build.zip","digest":"sha256:%s"},{"name":%q,"digest":%q}]}`,
			sum([]byte("lain")), name, digest)
	})
	mux.HandleFunc("/GyanD/codexffmpeg/releases/download/9.1/"+name, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	return &Manager{
		dir:              filepath.Join(dir, "tools"),
		tmpDir:           filepath.Join(dir, "tmp"),
		log:              slog.New(slog.NewTextHandler(io.Discard, nil)),
		client:           srv.Client(),
		cache:            make(map[string]cacheEntry),
		now:              time.Now,
		githubAPI:        srv.URL,
		githubDownload:   srv.URL,
		platformOverride: "windows/amd64",
	}
}

func TestInstallLatestFFmpegWindows(t *testing.T) {
	archive := zipOf(t, map[string]string{
		"ffmpeg-9.1-essentials_build/bin/ffmpeg.exe":  "ffmpeg baru",
		"ffmpeg-9.1-essentials_build/bin/ffprobe.exe": "ffprobe baru",
		"ffmpeg-9.1-essentials_build/README.txt":      "bukan yang dicari",
	})
	m := fakeFFmpegRelease(t, "sha256:"+sum(archive), archive)

	if !m.CanUpdate(FFmpeg) || m.CanUpdate(FFprobe) {
		t.Errorf("CanUpdate ffmpeg=%v ffprobe=%v, mau true dan false", m.CanUpdate(FFmpeg), m.CanUpdate(FFprobe))
	}
	version, err := m.InstallLatest(context.Background(), FFmpeg)
	if err != nil {
		t.Fatalf("InstallLatest() error = %v", err)
	}
	if version != "9.1" {
		t.Errorf("versi = %q, mau 9.1", version)
	}
	for file, want := range map[string]string{"ffmpeg": "ffmpeg baru", "ffprobe": "ffprobe baru"} {
		got, err := os.ReadFile(filepath.Join(m.dir, normalizeTarget(file)))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; mau %q", file, got, err, want)
		}
	}
}

// Digest GitHub tetap wajib cocok; arsip yang disusupi tidak pernah
// diekstrak.
func TestInstallLatestFFmpegMenolakChecksumSalah(t *testing.T) {
	archive := zipOf(t, map[string]string{"bin/ffmpeg.exe": "x", "bin/ffprobe.exe": "y"})
	m := fakeFFmpegRelease(t, "sha256:"+sum([]byte("arsip lain")), archive)

	_, err := m.InstallLatestFFmpeg(context.Background())
	wantCode(t, err, domain.CodeChecksumMismatch)
	if entries, _ := os.ReadDir(m.dir); len(entries) != 0 {
		t.Errorf("berkas terpasang walau checksum gagal: %d", len(entries))
	}
}

// Aset tanpa digest ditolak; menghitung hash dari unduhan yang sama tidak
// memverifikasi apa pun.
func TestInstallLatestFFmpegTanpaDigest(t *testing.T) {
	archive := zipOf(t, map[string]string{"bin/ffmpeg.exe": "x", "bin/ffprobe.exe": "y"})
	m := fakeFFmpegRelease(t, "", archive)

	_, err := m.InstallLatestFFmpeg(context.Background())
	wantCode(t, err, domain.CodeToolInstallFailed)
}

// Di luar Windows FFmpeg mengikuti manifest; tidak ada unduhan apa pun.
func TestInstallLatestFFmpegHanyaWindows(t *testing.T) {
	m := fakeFFmpegRelease(t, "", nil)
	m.platformOverride = "linux/amd64"

	if m.CanUpdate(FFmpeg) {
		t.Error("CanUpdate(ffmpeg) di Linux seharusnya false")
	}
	_, err := m.InstallLatest(context.Background(), FFmpeg)
	wantCode(t, err, domain.CodeToolManifest)
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

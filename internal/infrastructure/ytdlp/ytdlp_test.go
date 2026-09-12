package ytdlp

import (
	"slices"
	"strings"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

func TestCanonicalURL(t *testing.T) {
	got := CanonicalURL("youtube:dQw4w9WgXcQ")
	want := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	if got != want {
		t.Errorf("CanonicalURL = %q, mau %q", got, want)
	}
}

// Flag hardening di bawah adalah batas keamanan, bukan preferensi gaya:
// tanpa --ignore-config, berkas yt-dlp.conf milik pengguna dapat
// menyuntikkan --exec dan menjadikan ini jalur eksekusi sewenang-wenang.
func TestMetadataArgsMemuatFlagHardening(t *testing.T) {
	args := metadataArgs("https://www.youtube.com/watch?v=dQw4w9WgXcQ")

	for _, required := range []string{"--ignore-config", "--no-exec", "--no-playlist", "--skip-download"} {
		if !slices.Contains(args, required) {
			t.Errorf("argv tidak memuat %s", required)
		}
	}

	// URL wajib berada tepat setelah "--" supaya tidak pernah terbaca flag.
	sep := slices.Index(args, "--")
	if sep == -1 {
		t.Fatal("argv tidak memuat separator --")
	}
	if sep != len(args)-2 {
		t.Errorf("separator -- pada indeks %d, mau %d", sep, len(args)-2)
	}
	if !strings.HasPrefix(args[len(args)-1], "https://") {
		t.Errorf("argumen terakhir bukan URL: %q", args[len(args)-1])
	}
}

func TestMapStderr(t *testing.T) {
	tests := []struct {
		name      string
		stderr    string
		wantCode  domain.ErrorCode
		wantRetry bool
	}{
		{
			"video privat",
			"ERROR: [youtube] abc: Private video. Sign in if you've been granted access",
			domain.CodeVideoPrivate, false,
		},
		{
			"video dihapus",
			"ERROR: [youtube] abc: Video unavailable. This video has been removed by the uploader",
			domain.CodeVideoUnavailable, false,
		},
		{
			"geo block",
			"ERROR: [youtube] abc: The uploader has not made this video available in your country",
			domain.CodeGeoBlocked, false,
		},
		{
			"age gate",
			"ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users",
			domain.CodeAgeRestricted, false,
		},
		{
			"rate limit boleh diulang",
			"ERROR: unable to download video data: HTTP Error 429: Too Many Requests",
			domain.CodeRateLimited, true,
		},
		{
			"tool ketinggalan",
			"WARNING: [youtube] abc: nsig extraction failed: Some formats may be missing",
			domain.CodeToolOutdated, false,
		},
		{
			"jaringan transient",
			"ERROR: Unable to download webpage: <urlopen error timed out>",
			domain.CodeDownloadFailed, true,
		},
		{
			"tidak dikenal jadi transient",
			"ERROR: sesuatu yang belum pernah kita lihat",
			domain.CodeDownloadFailed, true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mapStderr([]byte(tc.stderr), 1)
			if err.Code != tc.wantCode {
				t.Errorf("kode = %s, mau %s", err.Code, tc.wantCode)
			}
			if err.Retryable() != tc.wantRetry {
				t.Errorf("Retryable() = %v, mau %v", err.Retryable(), tc.wantRetry)
			}
		})
	}
}

// Detail mentah dipangkas supaya log tidak dibanjiri keluaran panjang.
func TestMapStderrMemangkasDetail(t *testing.T) {
	long := "ERROR: " + strings.Repeat("x", 2000)
	err := mapStderr([]byte(long), 1)
	if len(err.Detail) > 600 {
		t.Errorf("detail sepanjang %d karakter, mau dipangkas", len(err.Detail))
	}
}

func TestDurationOf(t *testing.T) {
	tests := map[string]struct {
		in   float64
		want int64 // milidetik
	}{
		"normal":  {212.5, 212500},
		"nol":     {0, 0},
		"negatif": {-5, 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := durationOf(tc.in).Milliseconds()
			if got != tc.want {
				t.Errorf("durationOf(%v) = %dms, mau %dms", tc.in, got, tc.want)
			}
		})
	}
}

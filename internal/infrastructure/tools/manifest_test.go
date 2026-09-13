package tools

import (
	"encoding/hex"
	"runtime"
	"strings"
	"testing"
)

// releasePlatforms adalah seluruh target rilis; setiap tool wajib punya
// build ter-pin untuk masing-masing.
var releasePlatforms = []string{
	"windows/amd64", "linux/amd64", "linux/arm64",
	"darwin/amd64", "darwin/arm64",
}

func TestManifestTersematValid(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}

	for _, name := range []string{"yt-dlp", "ffmpeg"} {
		tool, ok := m.Tools[name]
		if !ok {
			t.Errorf("tool %q tidak ada di manifest", name)
			continue
		}
		if tool.Version == "" {
			t.Errorf("%s: versi belum di-pin", name)
		}
	}

	for toolName, tool := range m.Tools {
		for _, p := range releasePlatforms {
			b, ok := tool.Builds[p]
			if !ok {
				t.Errorf("%s: tidak ada entri build untuk %s", toolName, p)
				continue
			}
			if !b.Installable() {
				t.Errorf("%s/%s: belum di-pin; jalankan make update-tools", toolName, p)
			}
			for _, d := range b.Downloads {
				checkDownload(t, toolName+"/"+p, d)
			}
		}
	}
}

// checkDownload menangkap kesalahan penyuntingan manual yang tidak terlihat
// sampai instalasi gagal di mesin pengguna.
func checkDownload(t *testing.T, where string, d Download) {
	t.Helper()

	if !strings.HasPrefix(d.URL, "https://") {
		t.Errorf("%s: URL %q harus https", where, d.URL)
	}
	// Tag bergulir menunjuk isi yang berganti, sehingga checksum yang di-pin
	// pasti basi dalam hitungan hari.
	if strings.Contains(d.URL, "/latest/") {
		t.Errorf("%s: URL %q memakai rilis bergulir, bukan versi tetap", where, d.URL)
	}
	if raw, err := hex.DecodeString(d.SHA256); err != nil || len(raw) != 32 {
		t.Errorf("%s: sha256 %q bukan heksadesimal 64 karakter", where, d.SHA256)
	}
	switch d.Archive {
	case ArchiveNone:
		if len(d.Extract) != 1 {
			t.Errorf("%s: archive none harus punya tepat satu entri extract", where)
		}
	case ArchiveZip, ArchiveTarXZ:
	default:
		t.Errorf("%s: archive %q tidak dikenal", where, d.Archive)
	}
}

// Manifest yang belum di-pin harus menolak instalasi, bukan diam-diam
// mengunduh tanpa verifikasi.
func TestBuildBelumDipinTidakInstallable(t *testing.T) {
	ok := Download{URL: "https://x.test/a", SHA256: "abc", Extract: []string{"a"}}

	tests := []struct {
		name  string
		build Build
		want  bool
	}{
		{"lengkap", Build{Downloads: []Download{ok}}, true},
		{"tanpa unduhan", Build{}, false},
		{"tanpa checksum", Build{Downloads: []Download{{URL: "https://x.test/a", Extract: []string{"a"}}}}, false},
		{"tanpa url", Build{Downloads: []Download{{SHA256: "abc", Extract: []string{"a"}}}}, false},
		{"tanpa extract", Build{Downloads: []Download{{URL: "https://x.test/a", SHA256: "abc"}}}, false},
		{"satu dari dua belum di-pin", Build{Downloads: []Download{ok, {URL: "https://x.test/b", Extract: []string{"b"}}}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.build.Installable(); got != tc.want {
				t.Errorf("Installable() = %v, mau %v", got, tc.want)
			}
		})
	}
}

func TestPlatformKey(t *testing.T) {
	want := runtime.GOOS + "/" + runtime.GOARCH
	if got := platformKey(); got != want {
		t.Errorf("platformKey() = %q, mau %q", got, want)
	}
}

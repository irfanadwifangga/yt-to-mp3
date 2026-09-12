package tools

import (
	"runtime"
	"testing"
)

func TestManifestTersematValid(t *testing.T) {
	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error = %v", err)
	}

	for _, name := range []string{"yt-dlp", "ffmpeg"} {
		if _, ok := m.Tools[name]; !ok {
			t.Errorf("tool %q tidak ada di manifest", name)
		}
	}

	// Setiap platform rilis wajib punya entri, walau isinya belum di-pin.
	platforms := []string{
		"windows/amd64", "linux/amd64", "linux/arm64",
		"darwin/amd64", "darwin/arm64",
	}
	for toolName, tool := range m.Tools {
		for _, p := range platforms {
			b, ok := tool.Builds[p]
			if !ok {
				t.Errorf("%s: tidak ada entri build untuk %s", toolName, p)
				continue
			}
			if len(b.Extract) == 0 {
				t.Errorf("%s/%s: daftar extract kosong", toolName, p)
			}
			switch b.Archive {
			case ArchiveNone, ArchiveZip, ArchiveTarXZ:
			default:
				t.Errorf("%s/%s: archive %q tidak dikenal", toolName, p, b.Archive)
			}
		}
	}
}

// Manifest yang belum di-pin harus menolak instalasi, bukan diam-diam
// mengunduh tanpa verifikasi.
func TestBuildBelumDipinTidakInstallable(t *testing.T) {
	tests := []struct {
		name  string
		build Build
		want  bool
	}{
		{"lengkap", Build{URL: "https://x.test/a", SHA256: "abc", Extract: []string{"a"}}, true},
		{"tanpa checksum", Build{URL: "https://x.test/a", Extract: []string{"a"}}, false},
		{"tanpa url", Build{SHA256: "abc", Extract: []string{"a"}}, false},
		{"tanpa extract", Build{URL: "https://x.test/a", SHA256: "abc"}, false},
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

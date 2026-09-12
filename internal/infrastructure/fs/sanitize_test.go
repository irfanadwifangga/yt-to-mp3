package fs_test

import (
	"strings"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/fs"
)

func TestSanitizeNameKarakterIlegal(t *testing.T) {
	tests := map[string]struct {
		in   string
		want string
	}{
		"garis miring":     {`AC/DC - Live`, "AC_DC - Live"},
		"backslash":        {`path\to\file`, "path_to_file"},
		"titik dua":        {"10:30 Mix", "10_30 Mix"},
		"tanda tanya":      {"What? Really!", "What_ Really!"},
		"bintang":          {"Track *special*", "Track _special_"},
		"kutip":            {`Lagu "Terbaik"`, "Lagu _Terbaik_"},
		"pipa":             {"A|B", "A_B"},
		"kurung sudut":     {"<intro>", "_intro_"},
		"karakter kontrol": {"baris\x01satu", "baris_satu"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := fs.SanitizeName(tc.in, "fallback"); got != tc.want {
				t.Errorf("SanitizeName(%q) = %q, mau %q", tc.in, got, tc.want)
			}
		})
	}
}

// Non-ASCII bukan masalah; yang bermasalah hanya karakter yang dipesan
// sistem berkas. Judul Jepang dan emoji harus lolos utuh.
func TestSanitizeNameMempertahankanNonASCII(t *testing.T) {
	tests := []string{
		"back number - 水平線",
		"Lagu Indonesia — tanda pisah",
		"Track 🎵 dengan emoji",
		"Ñandú café",
		"Привет мир",
	}

	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			got := fs.SanitizeName(in, "fallback")
			if got != in {
				t.Errorf("SanitizeName(%q) = %q, seharusnya tidak berubah", in, got)
			}
		})
	}
}

// Windows membuang titik dan spasi di akhir secara diam-diam, sehingga nama
// yang tersimpan berbeda dari yang diminta.
func TestSanitizeNameMembuangTitikDanSpasiAkhir(t *testing.T) {
	tests := map[string]string{
		"Judul...":      "Judul",
		"Judul   ":      "Judul",
		"Judul . . ":    "Judul",
		"  Judul  Dua ": "Judul Dua",
	}
	for in, want := range tests {
		if got := fs.SanitizeName(in, "fallback"); got != want {
			t.Errorf("SanitizeName(%q) = %q, mau %q", in, got, want)
		}
	}
}

// Nama perangkat DOS masih dipesan Windows, termasuk ketika berekstensi.
func TestSanitizeNameNamaReserved(t *testing.T) {
	for _, in := range []string{"CON", "con", "PRN", "NUL", "COM1", "LPT9", "CON.mp3", "aux"} {
		t.Run(in, func(t *testing.T) {
			got := fs.SanitizeName(in, "fallback")
			if !strings.HasPrefix(got, "_") {
				t.Errorf("SanitizeName(%q) = %q, seharusnya diberi awalan", in, got)
			}
		})
	}

	// Nama yang hanya mirip tidak boleh ikut diubah.
	for _, in := range []string{"CONCERT", "COM10", "NULL", "PRNT"} {
		if got := fs.SanitizeName(in, "fallback"); got != in {
			t.Errorf("SanitizeName(%q) = %q, seharusnya tidak berubah", in, got)
		}
	}
}

func TestSanitizeNameKosongJatuhKeFallback(t *testing.T) {
	for _, in := range []string{"", "   ", "...", `///`} {
		got := fs.SanitizeName(in, "youtube_abc")
		if got == "" {
			t.Errorf("SanitizeName(%q) menghasilkan nama kosong", in)
		}
	}
}

func TestBuildFilename(t *testing.T) {
	info := &domain.MediaInfo{
		SourceKey: "youtube:iqEr3P78fz8",
		Title:     "水平線",
		Uploader:  "back number",
	}

	tests := map[domain.FilenameMode]string{
		domain.FilenameTitle:         "水平線.mp3",
		domain.FilenameTitleUploader: "水平線 - back number.mp3",
		domain.FilenameUploaderTitle: "back number - 水平線.mp3",
		domain.FilenameID:            "iqEr3P78fz8.mp3",
	}

	for mode, want := range tests {
		t.Run(string(mode), func(t *testing.T) {
			if got := fs.BuildFilename(mode, info, ".mp3"); got != want {
				t.Errorf("BuildFilename(%s) = %q, mau %q", mode, got, want)
			}
		})
	}
}

// Judul kosong harus jatuh ke id video, bukan menghasilkan berkas tanpa nama.
func TestBuildFilenameJudulKosong(t *testing.T) {
	info := &domain.MediaInfo{SourceKey: "youtube:iqEr3P78fz8"}
	if got := fs.BuildFilename(domain.FilenameTitle, info, ".mp3"); got != "iqEr3P78fz8.mp3" {
		t.Errorf("BuildFilename = %q, mau iqEr3P78fz8.mp3", got)
	}
}

// Pemotongan harus terjadi pada batas rune: memotong di tengah karakter
// multi-byte menghasilkan nama rusak, dan judul non-Latin hampir selalu
// multi-byte.
func TestBuildFilenameMemotongPadaBatasRune(t *testing.T) {
	long := strings.Repeat("水", 500)
	info := &domain.MediaInfo{SourceKey: "youtube:abcdefghijk", Title: long}

	got := fs.BuildFilename(domain.FilenameTitle, info, ".mp3")

	if len([]rune(got)) > 240 {
		t.Errorf("panjang %d rune, melebihi batas", len([]rune(got)))
	}
	if !strings.HasSuffix(got, ".mp3") {
		t.Errorf("ekstensi hilang setelah pemotongan: %q", got)
	}
	// Nama rusak akan memuat rune pengganti.
	if strings.ContainsRune(got, '\uFFFD') {
		t.Error("nama terpotong di tengah karakter")
	}
}

func TestFitToDirMemperhitungkanDirektori(t *testing.T) {
	dir := "C:\\Users\\Nama Panjang Sekali\\Music\\yt-to-mp3"
	name := strings.Repeat("a", 300)

	got := fs.FitToDir(dir, name, ".mp3")
	total := len([]rune(dir)) + 1 + len([]rune(got))

	if total > 245 {
		t.Errorf("total path %d karakter, terlalu panjang", total)
	}
}

func TestWithSuffix(t *testing.T) {
	tests := map[string]string{
		"lagu.mp3":       "lagu (2).mp3",
		"水平線.mp3":        "水平線 (2).mp3",
		"tanpa ekstensi": "tanpa ekstensi (2)",
		"a.b.c.mp3":      "a.b.c (2).mp3",
	}
	for in, want := range tests {
		if got := fs.WithSuffix(in, 2); got != want {
			t.Errorf("WithSuffix(%q) = %q, mau %q", in, got, want)
		}
	}
}

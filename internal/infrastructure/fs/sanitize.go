// Package fs menangani commit atomik, reservasi nama, sanitasi filename,
// dan pembersihan berkas temp.
package fs

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

const (
	// maxPathChars menyisakan margin dari batas 260 karakter Windows untuk
	// direktori keluaran, sufiks tabrakan, dan ekstensi.
	maxPathChars = 240

	// minNameChars mencegah pemotongan menghasilkan nama yang tidak berarti.
	minNameChars = 8
)

// illegalChars adalah karakter yang tidak boleh muncul pada nama berkas
// Windows. Daftar ini dipakai di semua OS supaya berkas yang dibuat di
// Linux tetap dapat disalin ke Windows.
const illegalChars = `<>:"/\|?*`

// reservedNames adalah nama perangkat DOS yang masih dipesan Windows.
//
// Berkas bernama "CON.mp3" tetap ditolak, jadi pemeriksaan dilakukan pada
// bagian sebelum titik, bukan pada nama utuh.
var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// SanitizeName mengubah judul bebas menjadi nama berkas yang aman.
//
// Aturan Windows dipakai di semua platform: itu target paling ketat, dan
// berkas yang aman di sana aman di mana pun. Karakter non-ASCII seperti
// aksara Jepang atau emoji dipertahankan, karena masalahnya bukan pada
// non-ASCII melainkan pada karakter yang dipesan sistem berkas.
func SanitizeName(title, fallback string) string {
	// NFC menyatukan bentuk komposisi yang berbeda, sehingga judul yang
	// tampak sama tidak menghasilkan dua berkas berbeda.
	name := norm.NFC.String(title)

	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f:
			b.WriteRune('_') // karakter kontrol
		case strings.ContainsRune(illegalChars, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name = b.String()

	// Windows membuang titik dan spasi di akhir secara diam-diam, sehingga
	// nama yang disimpan berbeda dari yang diminta.
	name = strings.TrimRight(name, ". ")
	name = strings.TrimSpace(name)
	name = collapseSpaces(name)

	if name == "" {
		name = fallback
	}
	if isReserved(name) {
		name = "_" + name
	}
	return name
}

// collapseSpaces merapikan spasi beruntun hasil penggantian karakter.
func collapseSpaces(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// isReserved memeriksa nama perangkat DOS pada bagian sebelum titik.
func isReserved(name string) bool {
	base, _, _ := strings.Cut(name, ".")
	return reservedNames[strings.ToUpper(base)]
}

// BuildFilename menyusun nama berkas akhir sesuai mode yang dipilih.
func BuildFilename(mode domain.FilenameMode, info *domain.MediaInfo, ext string) string {
	id := domain.VideoID(info.SourceKey)

	var raw string
	switch mode {
	case domain.FilenameTitleUploader:
		raw = joinNonEmpty(" - ", info.Title, info.Uploader)
	case domain.FilenameUploaderTitle:
		raw = joinNonEmpty(" - ", info.Uploader, info.Title)
	case domain.FilenameID:
		raw = id
	default:
		raw = info.Title
	}

	name := SanitizeName(raw, id)
	return truncateToLimit(name, ext, "")
}

func joinNonEmpty(sep string, parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// truncateToLimit memotong nama agar path akhir muat dalam batas.
//
// Pemotongan dilakukan pada batas rune, bukan byte: memotong di tengah
// karakter multi-byte menghasilkan nama rusak, dan judul non-Latin hampir
// selalu multi-byte.
func truncateToLimit(name, ext, dir string) string {
	budget := maxPathChars - len([]rune(dir)) - len([]rune(ext))
	if budget < minNameChars {
		budget = minNameChars
	}

	runes := []rune(name)
	if len(runes) > budget {
		runes = runes[:budget]
		name = strings.TrimRight(string(runes), ". ")
	}
	if name == "" {
		name = "audio"
	}
	return name + ext
}

// FitToDir memotong nama berkas agar path lengkapnya muat di direktori
// tertentu.
func FitToDir(dir, name, ext string) string {
	return truncateToLimit(name, ext, dir+string(filepath.Separator))
}

// WithSuffix menyisipkan sufiks penomoran sebelum ekstensi.
func WithSuffix(filename string, n int) string {
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)
	return fmt.Sprintf("%s (%d)%s", base, n, ext)
}

package domain

import (
	"regexp"
	"strings"
	"unicode"
)

// MaxTagRunes membatasi panjang judul dan artis suntingan pengguna. Tag ID3
// sendiri tidak punya batas praktis, tetapi nama berkas ikut diturunkan dari
// judul dan pemutar memotong teks sepanjang itu.
const MaxTagRunes = 200

// CleanTag menormalkan teks tag dari pengguna.
//
// Karakter kontrol dibuang karena baris baru di dalam tag merusak tampilan
// pemutar dan nama berkas, lalu spasi beruntun diringkas. Teks yang terlalu
// panjang dipotong pada batas rune; API menolaknya lebih dulu, jadi
// pemotongan di sini hanya jaring pengaman.
func CleanTag(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > MaxTagRunes {
		s = strings.TrimSpace(string(runes[:MaxTagRunes]))
	}
	return s
}

// noiseTokens adalah kata yang boleh muncul di kelompok penanda jenis
// unggahan, misalnya "(Official Video)" atau "(4K Remaster)".
var noiseTokens = map[string]bool{
	"official": true, "music": true, "lyric": true, "lyrics": true, "audio": true,
	"video": true, "videoclip": true, "clip": true, "mv": true, "m/v": true,
	"visualizer": true, "visualiser": true, "hd": true, "4k": true, "hq": true,
	"remaster": true, "remastered": true,
}

// weakTokens tidak cukup untuk menyebut sebuah kelompok penanda bila
// berdiri sendiri. "(Remastered)" atau "(Music)" bisa membedakan rekaman,
// sedangkan "(4K Remaster)" jelas soal videonya.
var weakTokens = map[string]bool{"music": true, "remaster": true, "remastered": true}

// isNoise melaporkan apakah isi kurung hanya menjelaskan jenis unggahan,
// bukan bagian dari judul lagu. Syaratnya ketat: setiap kata harus penanda
// dan minimal satu penanda kuat. "(Remix)", "(Live)", "(feat. X)", atau
// "(2011 Remaster)" membedakan satu rekaman dari yang lain dan tetap ada.
func isNoise(s string) bool {
	words := strings.Fields(strings.ToLower(s))
	strong := false
	for _, w := range words {
		if !noiseTokens[w] {
			return false
		}
		if !weakTokens[w] {
			strong = true
		}
	}
	return strong
}

// bracketed menangkap satu kelompok berkurung beserta spasi di depannya.
var bracketed = regexp.MustCompile(`\s*[(\[【]([^)\]】]*)[)\]】]`)

// pipeSuffix menangkap akhiran "| Official Video" yang lazim di judul.
var pipeSuffix = regexp.MustCompile(`\s*[|｜]\s*([^|｜]*)$`)

// titleSeparators memisahkan "Artis - Judul". Tanda pisah panjang ikut
// dikenali karena banyak kanal memakainya.
var titleSeparators = []string{" - ", " – ", " — "}

// SuggestTags menebak judul dan artis yang rapi dari metadata YouTube.
//
// Hasilnya hanya saran yang ditampilkan di formulir untuk dikoreksi
// pengguna, bukan nilai yang ditulis diam-diam. Tebakan "Artis - Judul"
// bisa keliru untuk judul seperti "Episode 5 - Penutup", dan karena itulah
// keputusannya dikembalikan ke pengguna.
func SuggestTags(title, uploader string) (suggestedTitle, suggestedArtist string) {
	t := bracketed.ReplaceAllStringFunc(title, func(group string) string {
		inner := bracketed.FindStringSubmatch(group)[1]
		if isNoise(inner) {
			return ""
		}
		return group
	})
	if m := pipeSuffix.FindStringSubmatch(t); m != nil && isNoise(m[1]) {
		t = t[:len(t)-len(m[0])]
	}
	t = CleanTag(t)

	artist := CleanTag(cleanUploader(uploader))
	for _, sep := range titleSeparators {
		// Hanya satu pemisah yang dianggap pasti. Dua atau lebih, misalnya
		// "A - B - C", terlalu ambigu untuk ditebak.
		if strings.Count(t, sep) != 1 {
			continue
		}
		left, right, _ := strings.Cut(t, sep)
		left, right = strings.TrimSpace(left), strings.TrimSpace(right)
		if left != "" && right != "" {
			return CleanTag(right), CleanTag(left)
		}
	}

	// Judul yang isinya hanya kata penanda tidak boleh berubah jadi kosong.
	if t == "" {
		t = CleanTag(title)
	}
	return t, artist
}

// cleanUploader membuang akhiran kanal otomatis YouTube Music ("Artis -
// Topic") dan kanal VEVO ("ArtisVEVO").
func cleanUploader(uploader string) string {
	u := strings.TrimSpace(uploader)
	u = strings.TrimSuffix(u, " - Topic")
	if len(u) > len("VEVO") && strings.HasSuffix(u, "VEVO") {
		u = strings.TrimSuffix(u, "VEVO")
	}
	return strings.TrimSpace(u)
}

// SuggestTagsFor memilih saran judul dan artis dari metadata sumber.
//
// Video yang terhubung ke YouTube Music membawa judul lagu dan artis dari
// katalog, yang jauh lebih bisa dipercaya daripada menebak dari judul video.
// Tebakan dari judul hanya dipakai untuk bagian yang tidak tersedia.
func SuggestTagsFor(m *MediaInfo) (title, artist string) {
	title, artist = SuggestTags(m.Title, m.Uploader)
	if t := CleanTag(m.Track); t != "" {
		title = t
	}
	if a := CleanTag(m.Artist); a != "" {
		artist = a
	}
	return title, artist
}

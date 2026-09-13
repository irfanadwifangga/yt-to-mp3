package domain

import (
	"strings"
	"testing"
)

func TestSuggestTags(t *testing.T) {
	tests := []struct {
		name, title, uploader string
		wantTitle, wantArtist string
	}{
		{"penanda video dibuang", "Queen - Bohemian Rhapsody (Official Video)", "Queen Official",
			"Bohemian Rhapsody", "Queen"},
		{"kurung siku dan kanal VEVO", "Dreams [Official Music Video]", "FleetwoodMacVEVO",
			"Dreams", "FleetwoodMac"},
		{"kanal Topic", "Kiss the Rain", "Yiruma - Topic", "Kiss the Rain", "Yiruma"},
		{"remix dan feat dipertahankan", "Get Lucky (feat. Pharrell Williams) (Remix)", "Daft Punk",
			"Get Lucky (feat. Pharrell Williams) (Remix)", "Daft Punk"},
		{"akhiran pipa", "Hati-Hati di Jalan | Official Lyric Video", "Tulus",
			"Hati-Hati di Jalan", "Tulus"},
		{"tanda pisah panjang", "YOASOBI – 夜に駆ける (Official Music Video)", "Ayase / YOASOBI",
			"夜に駆ける", "YOASOBI"},
		{"dua pemisah tidak ditebak", "A - B - C", "Kanal", "A - B - C", "Kanal"},
		{"hanya penanda tetap berjudul", "(Official Video)", "Kanal", "(Official Video)", "Kanal"},
		{"MV huruf kecil", "Butter [mv]", "HYBE LABELS", "Butter", "HYBE LABELS"},
		// Judul nyata dari YouTube yang dulu lolos pola pertama.
		{"remaster video dibuang", "Rick Astley - Never Gonna Give You Up (Official Video) (4K Remaster)",
			"Rick Astley", "Never Gonna Give You Up", "Rick Astley"},
		{"official video remastered", "Queen – Bohemian Rhapsody (Official Video Remastered)", "Queen Official",
			"Bohemian Rhapsody", "Queen"},
		{"remaster rekaman dipertahankan", "Here Comes the Sun (2019 Remaster)", "The Beatles",
			"Here Comes the Sun (2019 Remaster)", "The Beatles"},
		{"remastered saja dipertahankan", "Imagine (Remastered)", "John Lennon",
			"Imagine (Remastered)", "John Lennon"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotTitle, gotArtist := SuggestTags(tc.title, tc.uploader)
			if gotTitle != tc.wantTitle || gotArtist != tc.wantArtist {
				t.Errorf("SuggestTags(%q, %q) = %q, %q; mau %q, %q",
					tc.title, tc.uploader, gotTitle, gotArtist, tc.wantTitle, tc.wantArtist)
			}
		})
	}
}

func TestCleanTag(t *testing.T) {
	if got := CleanTag("  Judul\nbaru\t\x00 ini  "); got != "Judul baru ini" {
		t.Errorf("CleanTag() = %q", got)
	}
	long := strings.Repeat("水", MaxTagRunes+10)
	if got := []rune(CleanTag(long)); len(got) != MaxTagRunes {
		t.Errorf("panjang = %d rune, mau %d", len(got), MaxTagRunes)
	}
}

func TestMediaWithTags(t *testing.T) {
	src := &MediaInfo{Title: "Asli", Uploader: "Kanal"}

	job := &Job{}
	if got := job.MediaWithTags(src); got != src {
		t.Error("tanpa suntingan metadata seharusnya dipakai apa adanya")
	}

	job = &Job{TagTitle: "Suntingan"}
	got := job.MediaWithTags(src)
	if got.Title != "Suntingan" || got.Uploader != "Kanal" {
		t.Errorf("hasil = %+v", got)
	}
	// Salinan, bukan mutasi: metadata yang sama tersimpan di cache.
	if src.Title != "Asli" {
		t.Error("metadata sumber ikut berubah")
	}
}

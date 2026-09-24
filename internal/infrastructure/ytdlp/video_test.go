package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testURL = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

// Preset audio tidak boleh ikut mengunduh stream video (ADR-015), dan
// sampulnya tetap diambil untuk disematkan ke MP3.
func TestDownloadArgsAudio(t *testing.T) {
	args := downloadArgs(testURL, "/tmp/job", "/opt/ffmpeg", Selection{Thumbnail: true})

	if v := argAfter(args, "-f"); v != "bestaudio/best" {
		t.Errorf("-f = %q, mau bestaudio/best", v)
	}
	if !contains(args, "--write-thumbnail") {
		t.Error("unduhan audio harus mengambil sampul")
	}
	if contains(args, "-S") || contains(args, "--merge-output-format") {
		t.Errorf("unduhan audio tidak boleh memakai urutan format video: %v", args)
	}
}

// Resolusi mendahului codec: pilihan 720p harus menghasilkan 720p walau
// sumbernya hanya punya H.264 pada resolusi lebih rendah. H.264 dan AAC
// diutamakan supaya hasilnya cukup disalin ke MP4.
func TestDownloadArgsVideo(t *testing.T) {
	args := downloadArgs(testURL, "/tmp/job", "/opt/ffmpeg", Selection{
		Video: true, MaxHeight: 720, PreferVideo: "h264", PreferAudio: "aac",
	})

	if v := argAfter(args, "-f"); v != "bv*+ba/b" {
		t.Errorf("-f = %q, mau bv*+ba/b", v)
	}
	if v := argAfter(args, "-S"); v != "res:720,vcodec:h264,acodec:aac" {
		t.Errorf("-S = %q", v)
	}
	if v := argAfter(args, "--merge-output-format"); v != "mkv" {
		t.Errorf("--merge-output-format = %q, mau mkv", v)
	}
	if contains(args, "--write-thumbnail") {
		t.Error("unduhan video tidak perlu sampul terpisah")
	}

	// Hardening dan separator tetap berlaku di jalur video.
	for _, want := range []string{"--ignore-config", "--no-exec", "--no-playlist"} {
		if !contains(args, want) {
			t.Errorf("argv tidak memuat %s", want)
		}
	}
	if sep := indexOf(args, "--"); sep != len(args)-2 || args[len(args)-1] != testURL {
		t.Errorf("URL tidak tepat setelah separator: %v", args[len(args)-3:])
	}
}

func TestDownloadArgsVideoTerbaik(t *testing.T) {
	args := downloadArgs(testURL, "/tmp/job", "/opt/ffmpeg", Selection{
		Video: true, PreferVideo: "h264", PreferAudio: "aac",
	})

	if v := argAfter(args, "-S"); v != "res,vcodec:h264,acodec:aac" {
		t.Errorf("-S = %q, mau tanpa batas resolusi", v)
	}
}

// MKV menyimpan sumber apa adanya, jadi hanya resolusi yang diminta dan
// codec terbaik dibiarkan dipilih yt-dlp.
func TestDownloadArgsVideoTanpaPreferensiCodec(t *testing.T) {
	args := downloadArgs(testURL, "/tmp/job", "/opt/ffmpeg", Selection{Video: true, MaxHeight: 1080})

	if v := argAfter(args, "-S"); v != "res:1080" {
		t.Errorf("-S = %q, mau res:1080 saja", v)
	}
}

// Wadah audio tanpa dukungan sampul tidak perlu mengunduh thumbnail.
func TestDownloadArgsAudioTanpaSampul(t *testing.T) {
	args := downloadArgs(testURL, "/tmp/job", "/opt/ffmpeg", Selection{})

	if contains(args, "--write-thumbnail") {
		t.Errorf("thumbnail diunduh padahal tidak diminta: %v", args)
	}
}

func TestMaxVideoHeight(t *testing.T) {
	tests := map[string]struct {
		formats []rawFormat
		want    int
	}{
		"ambil tertinggi": {
			[]rawFormat{{VCodec: "avc1", Width: 1280, Height: 720}, {VCodec: "vp9", Width: 3840, Height: 2160}},
			2160,
		},
		// Storyboard bervcodec "none" namun tetap punya ukuran.
		"abaikan audio dan storyboard": {
			[]rawFormat{{VCodec: "none", Height: 0}, {VCodec: "none", Width: 3200, Height: 1800}, {VCodec: "avc1", Width: 640, Height: 360}},
			360,
		},
		// Label "p" adalah sisi terpendek, sama dengan urutan res yt-dlp.
		"video vertikal": {
			[]rawFormat{{VCodec: "avc1", Width: 1080, Height: 1920}},
			1080,
		},
		"tanpa lebar":  {[]rawFormat{{VCodec: "avc1", Height: 480}}, 480},
		"tanpa format": {nil, 0},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := maxVideoHeight(tc.formats); got != tc.want {
				t.Errorf("maxVideoHeight() = %d, mau %d", got, tc.want)
			}
		})
	}
}

func TestParseProgressStream(t *testing.T) {
	tests := map[string]struct {
		line string
		want Stream
	}{
		"video saja":       {"YTDLP_PROGRESS 1 10 NA avc1.640028 none", StreamVideo},
		"audio saja":       {"YTDLP_PROGRESS 1 10 NA none mp4a.40.2", StreamAudio},
		"berkas gabungan":  {"YTDLP_PROGRESS 1 10 NA avc1.42001E mp4a.40.2", StreamCombined},
		"codec tidak ada":  {"YTDLP_PROGRESS 1 10 NA NA NA", StreamUnknown},
		"format lama":      {"YTDLP_PROGRESS 1 10 NA", StreamUnknown},
		"video tanpa info": {"YTDLP_PROGRESS 1 10 NA NA none", StreamUnknown},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			p, ok := parseProgressLine(tc.line)
			if !ok {
				t.Fatalf("baris %q tidak terbaca", tc.line)
			}
			if p.Stream != tc.want {
				t.Errorf("Stream = %v, mau %v", p.Stream, tc.want)
			}
		})
	}
}

// Video dan audio diunduh sebagai dua berkas yang masing-masing melaporkan
// 0 sampai 100. Gabungannya tidak boleh mundur, apa pun urutannya.
func TestPartTrackerTidakMundur(t *testing.T) {
	orders := map[string][]Stream{
		"video dulu": {StreamVideo, StreamAudio},
		"audio dulu": {StreamAudio, StreamVideo},
	}
	for name, order := range orders {
		t.Run(name, func(t *testing.T) {
			var tr partTracker
			last := -1.0
			for _, s := range order {
				for _, done := range []int64{0, 250, 500, 1000} {
					pct := tr.place(Progress{Downloaded: done, Total: 1000, Stream: s}).Percent()
					if pct == nil {
						t.Fatal("Percent() nil padahal total diketahui")
					}
					if *pct < last {
						t.Fatalf("progres mundur dari %.1f ke %.1f", last, *pct)
					}
					last = *pct
				}
			}
			if last != 100 {
				t.Errorf("progres akhir = %.1f, mau 100", last)
			}
		})
	}
}

// Sumber yang tersedia sebagai satu berkas gabungan mencakup seluruh
// rentang progres.
func TestPartTrackerSatuBerkas(t *testing.T) {
	var tr partTracker
	pct := tr.place(Progress{Downloaded: 500, Total: 1000, Stream: StreamCombined}).Percent()
	if pct == nil || *pct != 50 {
		t.Errorf("Percent() = %v, mau 50", pct)
	}
}

// Setelah penggabungan, yang dipakai adalah berkas gabungannya; sisa bagian
// video dan audio yang belum terhapus tidak boleh terpilih.
func TestCollectOutputsMengabaikanBagianFormat(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"source.f137.mp4", "source.f251.webm", "source.mkv", "source.f140.m4a.part"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := collectOutputs(dir)
	if err != nil {
		t.Fatalf("collectOutputs() error = %v", err)
	}
	if filepath.Base(res.MediaPath) != "source.mkv" {
		t.Errorf("MediaPath = %s, mau source.mkv", res.MediaPath)
	}
	if res.ThumbnailPath != "" {
		t.Errorf("ThumbnailPath = %s, mau kosong", res.ThumbnailPath)
	}
}

func TestCollectOutputsAudio(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"source.webm", "source.jpg"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := collectOutputs(dir)
	if err != nil {
		t.Fatalf("collectOutputs() error = %v", err)
	}
	if !strings.HasSuffix(res.MediaPath, "source.webm") || !strings.HasSuffix(res.ThumbnailPath, "source.jpg") {
		t.Errorf("hasil = %+v", res)
	}
}

func argAfter(args []string, flag string) string {
	i := indexOf(args, flag)
	if i == -1 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

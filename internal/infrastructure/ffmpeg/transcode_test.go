package ffmpeg

import (
	"slices"
	"strings"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

func intPtr(v int) *int { return &v }

func cbrPreset() *domain.Preset {
	return &domain.Preset{
		ID: "mp3_standard", Codec: "libmp3lame", Mode: "cbr",
		BitrateKbps: intPtr(192), SampleRate: intPtr(48000), Channels: 2,
	}
}

func vbrPreset() *domain.Preset {
	return &domain.Preset{
		ID: "mp3_vbr_v0", Codec: "libmp3lame", Mode: "vbr",
		VBRQuality: intPtr(0), SampleRate: intPtr(48000), Channels: 2,
	}
}

// argValue mengembalikan nilai yang mengikuti sebuah flag.
func argValue(args []string, flag string) (string, bool) {
	i := slices.Index(args, flag)
	if i == -1 || i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

func TestBuildArgsCBR(t *testing.T) {
	args := BuildArgs(TranscodeInput{
		MediaPath:  "in.webm",
		OutputPath: "out.mp3",
		Preset:     cbrPreset(),
	})

	if v, _ := argValue(args, "-b:a"); v != "192k" {
		t.Errorf("-b:a = %q, mau 192k", v)
	}
	if slices.Contains(args, "-q:a") {
		t.Error("preset CBR tidak boleh memakai -q:a")
	}
	// 48 kHz adalah keputusan sadar menyesuaikan sumber Opus (ADR-030).
	if v, _ := argValue(args, "-ar"); v != "48000" {
		t.Errorf("-ar = %q, mau 48000", v)
	}
	if v, _ := argValue(args, "-ac"); v != "2" {
		t.Errorf("-ac = %q, mau 2", v)
	}
	if v, _ := argValue(args, "-id3v2_version"); v != "3" {
		t.Errorf("-id3v2_version = %q, mau 3", v)
	}
	if args[len(args)-1] != "out.mp3" {
		t.Errorf("argumen terakhir = %q, mau out.mp3", args[len(args)-1])
	}
}

func TestBuildArgsVBR(t *testing.T) {
	args := BuildArgs(TranscodeInput{
		MediaPath: "in.webm", OutputPath: "out.mp3", Preset: vbrPreset(),
	})

	if v, _ := argValue(args, "-q:a"); v != "0" {
		t.Errorf("-q:a = %q, mau 0", v)
	}
	if slices.Contains(args, "-b:a") {
		t.Error("preset VBR tidak boleh memakai -b:a")
	}
}

// sample_rate kosong berarti ikut sumber; preset lossless kehilangan
// maknanya bila di-resample.
func TestBuildArgsSampleRateIkutSumber(t *testing.T) {
	p := cbrPreset()
	p.SampleRate = nil

	args := BuildArgs(TranscodeInput{MediaPath: "in.flac", OutputPath: "out.flac", Preset: p})
	if slices.Contains(args, "-ar") {
		t.Error("sample_rate nil seharusnya tidak menghasilkan -ar")
	}
}

func TestBuildArgsSampul(t *testing.T) {
	t.Run("dengan sampul", func(t *testing.T) {
		args := BuildArgs(TranscodeInput{
			MediaPath: "in.webm", CoverPath: "cover.jpg",
			OutputPath: "out.mp3", Preset: cbrPreset(),
		})

		if !slices.Contains(args, "attached_pic") {
			t.Error("sampul tidak ditandai attached_pic")
		}
		if !slices.Contains(args, "1:v:0") {
			t.Error("stream sampul tidak dipetakan")
		}
	})

	t.Run("tanpa sampul", func(t *testing.T) {
		args := BuildArgs(TranscodeInput{
			MediaPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset(),
		})

		if slices.Contains(args, "attached_pic") {
			t.Error("tanpa sampul tidak boleh ada attached_pic")
		}
		// Tanpa input kedua, memetakan 1:v:0 membuat FFmpeg gagal.
		if slices.Contains(args, "1:v:0") {
			t.Error("tanpa sampul tidak boleh memetakan stream kedua")
		}
	})
}

// Tag kosong tampil sebagai entri hampa di pemutar, lebih buruk daripada
// tidak ada tag sama sekali.
func TestBuildArgsMelewatiTagKosong(t *testing.T) {
	args := BuildArgs(TranscodeInput{
		MediaPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset(),
		Media: &domain.MediaInfo{Title: "水平線", Uploader: "", SourceURL: "https://x.test"},
	})

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "title=水平線") {
		t.Error("judul tidak ditulis sebagai tag")
	}
	if strings.Contains(joined, "artist=") {
		t.Error("uploader kosong seharusnya tidak menghasilkan tag artist")
	}
	if !strings.Contains(joined, "comment=https://x.test") {
		t.Error("URL sumber tidak ditulis sebagai tag")
	}
}

func TestBuildArgsProgressKeStdout(t *testing.T) {
	args := BuildArgs(TranscodeInput{
		MediaPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset(),
	})

	if v, _ := argValue(args, "-progress"); v != "pipe:1" {
		t.Errorf("-progress = %q, mau pipe:1", v)
	}
	// Tanpa -nostats, FFmpeg membanjiri stderr dengan baris status.
	if !slices.Contains(args, "-nostats") {
		t.Error("-nostats tidak dipasang")
	}
}

// Sampul dipotong persegi dan dibatasi ukurannya; lihat coverFilter.
func TestBuildArgsSampulPersegi(t *testing.T) {
	args := BuildArgs(TranscodeInput{
		MediaPath: "in.webm", CoverPath: "cover.jpg",
		OutputPath: "out.mp3", Preset: cbrPreset(),
	})

	filter, ok := argValue(args, "-filter:v:0")
	if !ok {
		t.Fatal("sampul tidak difilter")
	}
	if !strings.HasPrefix(filter, `crop=min(iw\,ih):min(iw\,ih),`) ||
		!strings.Contains(filter, `scale=min(iw\,800):min(ih\,800)`) {
		t.Errorf("filter sampul = %q", filter)
	}
	if q, _ := argValue(args, "-q:v"); q != "2" {
		t.Errorf("-q:v = %q, mau 2", q)
	}

	noCover := BuildArgs(TranscodeInput{MediaPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset()})
	if slices.Contains(noCover, "-filter:v:0") {
		t.Error("tanpa sampul tidak boleh ada filter video")
	}
}

// Album dan tahun hanya ditulis dari data rilis. Nama kanal sebagai album
// mengelompokkan lagu tak berkaitan jadi satu album di pemutar.
func TestBuildArgsDataRilis(t *testing.T) {
	withRelease := strings.Join(BuildArgs(TranscodeInput{
		MediaPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset(),
		Media: &domain.MediaInfo{Title: "Kiss the Rain", Uploader: "YIRUMA place",
			Artist: "Yiruma", Album: "The Best", ReleaseYear: 2011},
	}), "\n")
	for _, want := range []string{"artist=Yiruma", "album=The Best", "date=2011"} {
		if !strings.Contains(withRelease, want) {
			t.Errorf("argv tidak memuat %q", want)
		}
	}

	plain := strings.Join(BuildArgs(TranscodeInput{
		MediaPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset(),
		Media: &domain.MediaInfo{Title: "Bohemian Rhapsody", Uploader: "Queen Official"},
	}), "\n")
	if !strings.Contains(plain, "artist=Queen Official") {
		t.Error("tanpa data katalog, artist seharusnya jatuh ke uploader")
	}
	if strings.Contains(plain, "album=") || strings.Contains(plain, "date=") {
		t.Error("tanpa data rilis tidak boleh ada tag album maupun tahun")
	}
}

func mp4Preset() *domain.Preset {
	return &domain.Preset{
		ID: "mp4_720", Kind: domain.KindVideo, Format: "mp4", Codec: "aac", Mode: "cbr",
		BitrateKbps: intPtr(192), Channels: 2, MaxHeight: intPtr(720),
	}
}

// Sumber yang sudah H.264 dan AAC cukup disalin: lebih cepat berkali lipat
// dan tanpa kehilangan kualitas.
func TestBuildArgsVideoSalin(t *testing.T) {
	args := BuildArgs(TranscodeInput{
		MediaPath: "in.mkv", OutputPath: "out.mp4", Preset: mp4Preset(),
		Media:     &domain.MediaInfo{Title: "Judul", Uploader: "Kanal"},
		CopyVideo: true, CopyAudio: true,
	})

	if v, _ := argValue(args, "-c:v"); v != "copy" {
		t.Errorf("-c:v = %q, mau copy", v)
	}
	if v, _ := argValue(args, "-c:a"); v != "copy" {
		t.Errorf("-c:a = %q, mau copy", v)
	}
	for _, flag := range []string{"-crf", "-b:a", "-ar", "-ac", "-id3v2_version"} {
		if slices.Contains(args, flag) {
			t.Errorf("salinan stream tidak boleh memakai %s", flag)
		}
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-map 0:v:0 -map 0:a:0") {
		t.Errorf("stream yang dipetakan salah: %v", args)
	}
	if v, _ := argValue(args, "-movflags"); v != "+faststart" {
		t.Errorf("-movflags = %q, mau +faststart", v)
	}
	if !slices.Contains(args, "title=Judul") || !slices.Contains(args, "artist=Kanal") {
		t.Errorf("tag judul dan artis tidak ditulis: %v", args)
	}
	if args[len(args)-1] != "out.mp4" {
		t.Errorf("argumen terakhir = %q, mau out.mp4", args[len(args)-1])
	}
}

// VP9, AV1, dan Opus di-encode ulang ke H.264 8-bit dan AAC supaya hasilnya
// diputar di mana saja.
func TestBuildArgsVideoEncodeUlang(t *testing.T) {
	args := BuildArgs(TranscodeInput{
		MediaPath: "in.mkv", OutputPath: "out.mp4", Preset: mp4Preset(),
	})

	want := map[string]string{
		"-c:v": "libx264", "-preset": "veryfast", "-crf": "20", "-pix_fmt": "yuv420p",
		"-c:a": "aac", "-b:a": "192k", "-ac": "2",
	}
	for flag, value := range want {
		if v, _ := argValue(args, flag); v != value {
			t.Errorf("%s = %q, mau %q", flag, v, value)
		}
	}
	// Audio yang menyertai video ikut sample rate sumber.
	if slices.Contains(args, "-ar") {
		t.Error("preset video tanpa sample_rate tidak boleh me-resample")
	}
}

func TestProbeResultMP4Ready(t *testing.T) {
	tests := []struct {
		name       string
		res        ProbeResult
		video, aud bool
	}{
		{"h264 aac", ProbeResult{VideoCodec: "h264", PixFmt: "yuv420p", Codec: "aac"}, true, true},
		{"vp9 opus", ProbeResult{VideoCodec: "vp9", PixFmt: "yuv420p", Codec: "opus"}, false, false},
		{"av1", ProbeResult{VideoCodec: "av1", PixFmt: "yuv420p", Codec: "aac"}, false, true},
		// H.264 10-bit sah, tetapi banyak pemutar perangkat keras menolaknya.
		{"h264 10-bit", ProbeResult{VideoCodec: "h264", PixFmt: "yuv420p10le", Codec: "aac"}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.res.MP4ReadyVideo(); got != tc.video {
				t.Errorf("MP4ReadyVideo() = %v, mau %v", got, tc.video)
			}
			if got := tc.res.MP4ReadyAudio(); got != tc.aud {
				t.Errorf("MP4ReadyAudio() = %v, mau %v", got, tc.aud)
			}
		})
	}
}

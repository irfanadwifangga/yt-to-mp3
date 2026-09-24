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
		ID: "mp3_standard", Format: "mp3", Codec: "libmp3lame", Mode: "cbr",
		BitrateKbps: intPtr(192), SampleRate: intPtr(48000), Channels: 2,
	}
}

func vbrPreset() *domain.Preset {
	return &domain.Preset{
		ID: "mp3_vbr_v0", Format: "mp3", Codec: "libmp3lame", Mode: "vbr",
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

func TestCopyPlan(t *testing.T) {
	h264 := &ProbeResult{HasVideo: true, VideoCodec: "h264", PixFmt: "yuv420p", HasAudio: true, Codec: "aac"}
	vp9 := &ProbeResult{HasVideo: true, VideoCodec: "vp9", PixFmt: "yuv420p", HasAudio: true, Codec: "opus"}
	h264Hi10 := &ProbeResult{HasVideo: true, VideoCodec: "h264", PixFmt: "yuv420p10le", HasAudio: true, Codec: "aac"}
	opus := &ProbeResult{HasAudio: true, Codec: "opus"}
	aac := &ProbeResult{HasAudio: true, Codec: "aac"}

	video := func(format string) *domain.Preset {
		return &domain.Preset{Kind: domain.KindVideo, Format: format, Passthrough: true}
	}
	tests := []struct {
		name         string
		preset       *domain.Preset
		src          *ProbeResult
		video, audio bool
	}{
		{"mp4 dari h264 aac", video("mp4"), h264, true, true},
		{"mp4 dari vp9 opus", video("mp4"), vp9, false, false},
		// H.264 10-bit sah, tetapi banyak pemutar perangkat keras menolaknya.
		{"mp4 dari h264 10-bit", video("mp4"), h264Hi10, false, true},
		{"mov dari h264 aac", video("mov"), h264, true, true},
		{"flv dari vp9 opus", video("flv"), vp9, false, false},
		{"webm dari vp9 opus", video("webm"), vp9, true, true},
		{"webm dari h264 aac", video("webm"), h264, false, false},
		// MKV menampung apa pun, termasuk H.264 10-bit.
		{"mkv dari vp9 opus", video("mkv"), vp9, true, true},
		{"mkv dari h264 10-bit", video("mkv"), h264Hi10, true, true},
		{"avi dari h264", video("avi"), h264, false, false},
		{"opus asli dari opus", &domain.Preset{Format: "opus", Passthrough: true}, opus, false, true},
		{"opus asli dari aac", &domain.Preset{Format: "opus", Passthrough: true}, aac, false, false},
		// Preset tanpa passthrough selalu di-encode ke kualitas pilihannya.
		{"m4a dari aac", &domain.Preset{Format: "m4a"}, aac, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, ok := domain.FormatOf(tc.preset.Format)
			if !ok {
				t.Fatalf("format %s tidak dikenal", tc.preset.Format)
			}
			v, a := copyPlan(tc.preset, f, tc.src)
			if v != tc.video || a != tc.audio {
				t.Errorf("copyPlan() = video %v audio %v, mau %v %v", v, a, tc.video, tc.audio)
			}
		})
	}
}

func TestBuildArgsPerFormat(t *testing.T) {
	video := func(format, codec string) *domain.Preset {
		return &domain.Preset{
			Kind: domain.KindVideo, Format: format, Codec: codec, Mode: domain.ModeCBR,
			BitrateKbps: intPtr(160), Channels: 2, Passthrough: true,
		}
	}
	lossless := func(format, codec string) *domain.Preset {
		return &domain.Preset{Format: format, Codec: codec, Mode: domain.ModeLossless, Channels: 2}
	}
	tests := []struct {
		name    string
		in      TranscodeInput
		want    map[string]string
		without []string
	}{
		{
			"webm encode ulang ke vp9",
			TranscodeInput{Preset: video("webm", "libopus")},
			map[string]string{"-c:v": "libvpx-vp9", "-deadline": "realtime", "-c:a": "libopus", "-b:a": "160k"},
			[]string{"-movflags", "-id3v2_version"},
		},
		{
			"avi xvid dan mp3",
			TranscodeInput{Preset: video("avi", "libmp3lame")},
			map[string]string{"-c:v": "mpeg4", "-vtag": "xvid", "-c:a": "libmp3lame"},
			[]string{"-movflags"},
		},
		{
			"mov salin dengan faststart",
			TranscodeInput{Preset: video("mov", "aac"), CopyVideo: true, CopyAudio: true},
			map[string]string{"-c:v": "copy", "-c:a": "copy", "-movflags": "+faststart"},
			[]string{"-b:a"},
		},
		{
			"flac 16-bit dengan sampul",
			TranscodeInput{Preset: lossless("flac", "flac"), CoverPath: "cover.jpg"},
			map[string]string{"-c:a": "flac", "-sample_fmt": "s16", "-disposition:v:0": "attached_pic"},
			[]string{"-b:a", "-q:a", "-ar", "-id3v2_version"},
		},
		{
			"alac di m4a",
			TranscodeInput{Preset: lossless("m4a", "alac")},
			map[string]string{"-c:a": "alac", "-sample_fmt": "s16p", "-movflags": "+faststart"},
			[]string{"-b:a"},
		},
		// WAV dan Ogg tidak menyematkan sampul walau thumbnail tersedia.
		{
			"wav tanpa sampul",
			TranscodeInput{Preset: lossless("wav", "pcm_s16le"), CoverPath: "cover.jpg"},
			map[string]string{"-c:a": "pcm_s16le"},
			[]string{"-disposition:v:0", "-sample_fmt", "-b:a"},
		},
		{
			"opus disalin",
			TranscodeInput{Preset: &domain.Preset{Format: "opus", Codec: "libopus", Mode: domain.ModeCBR, BitrateKbps: intPtr(160), Passthrough: true}, CopyAudio: true},
			map[string]string{"-c:a": "copy"},
			[]string{"-b:a", "-ac"},
		},
		{
			"vorbis vbr",
			TranscodeInput{Preset: &domain.Preset{Format: "ogg", Codec: "libvorbis", Mode: domain.ModeVBR, VBRQuality: intPtr(6), Channels: 2}},
			map[string]string{"-c:a": "libvorbis", "-q:a": "6"},
			[]string{"-b:a"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.in.MediaPath, tc.in.OutputPath = "in.mkv", "out"
			args := BuildArgs(tc.in)
			for flag, value := range tc.want {
				if v, _ := argValue(args, flag); v != value {
					t.Errorf("%s = %q, mau %q", flag, v, value)
				}
			}
			for _, flag := range tc.without {
				if slices.Contains(args, flag) {
					t.Errorf("argv tidak boleh memuat %s: %v", flag, args)
				}
			}
		})
	}
}

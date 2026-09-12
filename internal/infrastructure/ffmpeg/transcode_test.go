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
		AudioPath:  "in.webm",
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
		AudioPath: "in.webm", OutputPath: "out.mp3", Preset: vbrPreset(),
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

	args := BuildArgs(TranscodeInput{AudioPath: "in.flac", OutputPath: "out.flac", Preset: p})
	if slices.Contains(args, "-ar") {
		t.Error("sample_rate nil seharusnya tidak menghasilkan -ar")
	}
}

func TestBuildArgsSampul(t *testing.T) {
	t.Run("dengan sampul", func(t *testing.T) {
		args := BuildArgs(TranscodeInput{
			AudioPath: "in.webm", CoverPath: "cover.jpg",
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
			AudioPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset(),
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
		AudioPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset(),
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
		AudioPath: "in.webm", OutputPath: "out.mp3", Preset: cbrPreset(),
	})

	if v, _ := argValue(args, "-progress"); v != "pipe:1" {
		t.Errorf("-progress = %q, mau pipe:1", v)
	}
	// Tanpa -nostats, FFmpeg membanjiri stderr dengan baris status.
	if !slices.Contains(args, "-nostats") {
		t.Error("-nostats tidak dipasang")
	}
}

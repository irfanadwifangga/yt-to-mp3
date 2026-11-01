//go:build integration

package ffmpeg_test

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ffmpeg"
)

func TestIntegrasiTrimPrecise(t *testing.T) {
	e := setup(t)
	input := filepath.Join(e.dir, "source.mp4")
	output, err := exec.Command(e.ffmpeg, "-hide_banner", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=30:duration=5", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration=5", "-c:v", "libx264", "-g", "60", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", input).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}
	start, end := int64(1250), int64(3500)
	for _, tc := range []struct {
		format, codec string
		kind          domain.PresetKind
	}{
		{"mp3", "libmp3lame", domain.KindAudio},
		{"opus", "copy", domain.KindAudio},
		{"wav", "pcm_s16le", domain.KindAudio},
		{"mkv", "copy", domain.KindVideo},
		{"mp4", "aac", domain.KindVideo},
	} {
		t.Run(tc.format, func(t *testing.T) {
			out := filepath.Join(e.dir, "trim."+tc.format)
			preset := &domain.Preset{ID: tc.format, Format: tc.format, Codec: tc.codec, Kind: tc.kind, Mode: domain.ModeCBR, BitrateKbps: intPtr(192), Channels: 2, Passthrough: true}
			if tc.format == "wav" {
				preset.Mode = domain.ModeLossless
				preset.BitrateKbps = nil
			}
			err := ffmpeg.NewTranscoder(e.tools, e.log).Transcode(context.Background(), ffmpeg.TranscodeInput{MediaPath: input, OutputPath: out, Preset: preset, Media: media(), TrimStartMS: &start, TrimEndMS: &end, Timeout: time.Minute}, func(ffmpeg.Progress) {})
			if err != nil {
				t.Fatal(err)
			}
			probe, err := ffmpeg.NewProber(e.tools, e.log).Probe(context.Background(), out)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(probe.Duration.Seconds()-2.25) > 0.12 {
				t.Fatalf("trim duration=%s, want 2.25s within codec/frame tolerance", probe.Duration)
			}
			if !probe.HasAudio || (tc.kind == domain.KindVideo && !probe.HasVideo) {
				t.Fatalf("missing streams: %+v", probe)
			}
			if tc.format == "mkv" && (probe.VideoCodec != "h264" || probe.Codec != "aac") {
				t.Fatalf("trimmed MKV codecs: %+v", probe)
			}
			if tc.format == "opus" && probe.Codec != "opus" {
				t.Fatalf("trimmed Opus codec=%s", probe.Codec)
			}
		})
	}
}

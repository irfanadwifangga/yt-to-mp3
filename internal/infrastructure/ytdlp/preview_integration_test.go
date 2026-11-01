//go:build integration

package ytdlp

import (
	"context"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ffmpeg"
	"io"
	"log/slog"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type previewTestTools struct{}

func (previewTestTools) Resolve(_ context.Context, name string) (string, string, error) {
	p, e := exec.LookPath(name)
	return p, "test", e
}
func TestIntegrasiLocalPreviewEncoding(t *testing.T) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "preview.mp4")
	result, err := exec.Command(bin, "-hide_banner", "-y", "-f", "lavfi", "-i", "testsrc2=size=640x480:rate=25:duration=2", "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", input).CombinedOutput()
	if err != nil {
		t.Fatalf("fixture: %v %s", err, result)
	}
	result, err = exec.Command(bin, previewEncodeArgs(input, output)...).CombinedOutput()
	if err != nil {
		t.Fatalf("preview: %v %s", err, result)
	}
	p, err := ffmpeg.NewProber(previewTestTools{}, slog.New(slog.NewTextHandler(io.Discard, nil))).Probe(context.Background(), output)
	if err != nil {
		t.Fatal(err)
	}
	if !p.HasVideo || !p.HasAudio || p.VideoCodec != "h264" || p.Codec != "aac" || math.Abs(p.Duration.Seconds()-2) > .1 {
		t.Fatalf("invalid preview: %+v", p)
	}
	result, err = exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=height", "-of", "default=noprint_wrappers=1:nokey=1", output).Output()
	if err != nil || strings.TrimSpace(string(result)) != "360" {
		t.Fatalf("height: %s %v", result, err)
	}
}

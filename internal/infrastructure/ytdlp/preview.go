package ytdlp

import (
	"context"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ffmpeg"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
	"github.com/irfanadwifangga/yt-to-mp3/internal/preview"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
)

type PreviewBuilder struct {
	Tools ToolProvider
	Log   *slog.Logger
}

func previewEncodeArgs(input, output string) []string {
	return []string{"-hide_banner", "-nostdin", "-y", "-i", input, "-map", "0:v:0", "-map", "0:a:0?", "-vf", `scale=-2:trunc(min(360\,ih)/2)*2`, "-c:v", "libx264", "-preset", "ultrafast", "-crf", "28", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "96k", "-ac", "2", "-movflags", "+faststart", "-fs", "83886080", "-progress", "pipe:1", "-nostats", output}
}
func (b PreviewBuilder) Build(ctx context.Context, url, dir string, report func(string, *float64)) error {
	key, invalid := domain.NormalizeURL(url)
	if invalid != nil {
		return invalid
	}
	source, err := NewDownloader(b.Tools, b.Log).Download(ctx, DownloadInput{SourceKey: key, TempDir: dir, MaxBytes: preview.MaxFileBytes, Selection: Selection{Video: true, MaxHeight: 360, PreferVideo: "h264", PreferAudio: "aac"}}, func(p Progress) { report("downloading", p.Percent()) })
	if err != nil {
		return err
	}
	info, err := os.Stat(source.MediaPath)
	if err != nil {
		return err
	}
	if info.Size() > preview.MaxFileBytes {
		return domain.NewError(domain.CodePreviewTooLarge, domain.ClassLocal, "sumber preview terlalu besar")
	}
	probe := ffmpeg.NewProber(b.Tools, b.Log)
	original, err := probe.Probe(ctx, source.MediaPath)
	if err != nil {
		return err
	}
	if !original.HasVideo || original.Duration <= 0 {
		return domain.NewError(domain.CodeVerifyFailed, domain.ClassLocal, "sumber preview tidak memiliki video berdurasi")
	}
	bin, _, err := b.Tools.Resolve(ctx, tools.FFmpeg)
	if err != nil {
		return err
	}
	report("encoding", nil)
	output := filepath.Join(dir, "preview.mp4")
	handle, err := process.Start(ctx, process.Spec{Bin: bin, Args: previewEncodeArgs(source.MediaPath, output)})
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = ffmpeg.ParseProgress(handle.Stdout, func(p ffmpeg.Progress) { report("encoding", p.Percent(original.Duration)) })
	}()
	go func() { defer wg.Done(); _, _ = io.Copy(io.Discard, handle.Stderr) }()
	wg.Wait()
	err = handle.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || handle.ExitCode() != 0 {
		return domain.NewError(domain.CodeTranscodeFailed, domain.ClassLocal, "konversi preview gagal")
	}
	result, err := probe.Probe(ctx, output)
	if err != nil {
		return err
	}
	info, err = os.Stat(output)
	if err != nil {
		return err
	}
	if info.Size() > preview.MaxFileBytes || math.Abs(float64(result.Duration-original.Duration)) > float64(500000000) {
		if info.Size() > preview.MaxFileBytes-(512<<10) {
			return domain.NewError(domain.CodePreviewTooLarge, domain.ClassLocal, "preview dipotong oleh batas ukuran")
		}
		return domain.NewError(domain.CodeVerifyFailed, domain.ClassLocal, "durasi preview tidak sesuai sumber")
	}
	if !result.HasVideo || result.VideoCodec != "h264" || (original.HasAudio && (!result.HasAudio || result.Codec != "aac")) {
		return domain.NewError(domain.CodeVerifyFailed, domain.ClassLocal, "codec preview tidak sesuai")
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.Name() != "preview.mp4" {
			_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}

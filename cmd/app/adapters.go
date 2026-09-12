package main

import (
	"context"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ffmpeg"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/fs"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ytdlp"
)

// Adapter di bawah menjembatani tipe konkret infrastructure dengan port
// application. Keduanya sengaja tidak saling mengenal: port memakai tipe
// dasar supaya application tetap bebas dari detail tool. Perakitannya
// menjadi tanggung jawab lapisan wiring, yaitu berkas ini.

type downloaderAdapter struct{ inner *ytdlp.Downloader }

func (a downloaderAdapter) Download(
	ctx context.Context, req application.DownloadRequest, onProgress func(*float64),
) (*application.DownloadOutcome, error) {
	res, err := a.inner.Download(ctx, ytdlp.DownloadInput{
		SourceKey: req.SourceKey,
		TempDir:   req.TempDir,
		Timeout:   req.Timeout,
	}, func(p ytdlp.Progress) {
		onProgress(p.Percent())
	})
	if err != nil {
		return nil, err
	}
	return &application.DownloadOutcome{
		AudioPath: res.AudioPath,
		CoverPath: res.ThumbnailPath,
	}, nil
}

type transcoderAdapter struct{ inner *ffmpeg.Transcoder }

func (a transcoderAdapter) Transcode(
	ctx context.Context, req application.TranscodeRequest, onProgress func(*float64),
) error {
	// Durasi sumber dibutuhkan untuk mengubah posisi encoding jadi persen;
	// bila tidak diketahui, Percent mengembalikan nil dan UI menampilkan
	// indikator indeterminate.
	var total = req.Media.Duration

	return a.inner.Transcode(ctx, ffmpeg.TranscodeInput{
		AudioPath:  req.AudioPath,
		CoverPath:  req.CoverPath,
		OutputPath: req.OutputPath,
		Preset:     req.Preset,
		Media:      req.Media,
		Timeout:    req.Timeout,
	}, func(p ffmpeg.Progress) {
		onProgress(p.Percent(total))
	})
}

type namingAdapter struct{}

func (namingAdapter) Build(mode domain.FilenameMode, info *domain.MediaInfo, ext string) string {
	return fs.BuildFilename(mode, info, ext)
}

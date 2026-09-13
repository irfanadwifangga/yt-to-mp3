// Package adapters menjembatani tipe konkret infrastructure dengan port
// application.
//
// Keduanya sengaja tidak saling mengenal: port memakai tipe dasar supaya
// application tetap bebas dari detail tool. Adapter tinggal di paket
// tersendiri, bukan di cmd/app, supaya test E2E merakit pipeline dengan
// adapter yang persis sama dengan aplikasi alih-alih salinannya.
package adapters

import (
	"context"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/browser"
	"github.com/irfanadwifangga/yt-to-mp3/internal/dialog"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ffmpeg"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/fs"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ytdlp"
)

// Downloader memenuhi application.Downloader dengan yt-dlp.
type Downloader struct{ Inner *ytdlp.Downloader }

// Download menjalankan unduhan dan menerjemahkan progresnya ke persen.
func (a Downloader) Download(
	ctx context.Context, req application.DownloadRequest, onProgress func(*float64),
) (*application.DownloadOutcome, error) {
	res, err := a.Inner.Download(ctx, ytdlp.DownloadInput{
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

// Transcoder memenuhi application.Transcoder dengan FFmpeg.
type Transcoder struct{ Inner *ffmpeg.Transcoder }

// Transcode menjalankan konversi dan menerjemahkan posisinya ke persen.
func (a Transcoder) Transcode(
	ctx context.Context, req application.TranscodeRequest, onProgress func(*float64),
) error {
	// Durasi sumber dibutuhkan untuk mengubah posisi encoding jadi persen;
	// bila tidak diketahui, Percent mengembalikan nil dan UI menampilkan
	// indikator indeterminate.
	total := req.Media.Duration

	return a.Inner.Transcode(ctx, ffmpeg.TranscodeInput{
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

// Naming memenuhi application.FilenameBuilder.
type Naming struct{}

// Build menyusun nama berkas keluaran.
func (Naming) Build(mode domain.FilenameMode, info *domain.MediaInfo, ext string) string {
	return fs.BuildFilename(mode, info, ext)
}

// Revealer membuka lokasi berkas di file manager.
type Revealer struct{}

// Reveal menampilkan berkas di file manager sistem.
func (Revealer) Reveal(path string) error { return browser.Reveal(path) }

// Picker membuka dialog pemilih folder native.
type Picker struct{}

// PickFolder membuka dialog dan melaporkan apakah pengguna membatalkannya.
func (Picker) PickFolder(ctx context.Context, title, start string) (string, bool, error) {
	return dialog.PickFolder(ctx, title, start)
}

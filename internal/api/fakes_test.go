package api_test

import (
	"context"
	"io"
	"log/slog"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// newDiscardLogger membuat logger yang membuang seluruh output agar keluaran
// test tetap bersih.
func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeTools menggantikan tool manager sungguhan supaya test tidak pernah
// menyentuh filesystem maupun jaringan.
type fakeTools struct {
	status     map[string]application.ToolStatus
	installed  []string
	installErr error
}

func newFakeTools() *fakeTools {
	return &fakeTools{
		status: map[string]application.ToolStatus{
			"yt-dlp":  {Name: "yt-dlp", Available: true, Version: "2026.01.01", Source: "managed"},
			"ffmpeg":  {Name: "ffmpeg", Available: false},
			"ffprobe": {Name: "ffprobe", Available: false},
		},
	}
}

func (f *fakeTools) StatusAll(context.Context) map[string]application.ToolStatus {
	return f.status
}

func (f *fakeTools) Install(_ context.Context, name string) error {
	if f.installErr != nil {
		return f.installErr
	}
	f.installed = append(f.installed, name)
	return nil
}

// fakeResolver mencatat source key yang diminta sehingga test dapat
// memastikan validasi URL terjadi sebelum resolver dipanggil.
type fakeResolver struct {
	info    *domain.MediaInfo
	err     error
	calls   int
	lastKey string
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{
		info: &domain.MediaInfo{
			SourceKey:   "youtube:dQw4w9WgXcQ",
			SourceURL:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			Title:       "Judul Contoh",
			Uploader:    "Channel Contoh",
			DurationMS:  212000,
			SourceCodec: "opus",
			SampleRate:  48000,
		},
	}
}

func (f *fakeResolver) Resolve(_ context.Context, sourceKey string) (*domain.MediaInfo, error) {
	f.calls++
	f.lastKey = sourceKey
	if f.err != nil {
		return nil, f.err
	}
	return f.info, nil
}

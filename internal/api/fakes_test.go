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

// fakeCache adalah cache metadata in-memory; selalu meleset kecuali test
// mengisinya lebih dulu.
type fakeCache struct {
	items map[string]*domain.MediaInfo
	puts  int
}

func newFakeCache() *fakeCache {
	return &fakeCache{items: map[string]*domain.MediaInfo{}}
}

func (c *fakeCache) Get(_ context.Context, key string) (*domain.MediaInfo, bool, error) {
	info, ok := c.items[key]
	return info, ok, nil
}

func (c *fakeCache) Upsert(_ context.Context, info *domain.MediaInfo, _ string) error {
	c.puts++
	c.items[info.SourceKey] = info
	return nil
}

// fakePresets menggantikan repository preset.
type fakePresets struct {
	presets []domain.Preset
	err     error
}

func newFakePresets() *fakePresets {
	rate := 48000
	bitrate := 192
	return &fakePresets{presets: []domain.Preset{{
		ID: "mp3_standard", Label: "Standard", Format: "mp3", Codec: "libmp3lame",
		Mode: "cbr", BitrateKbps: &bitrate, SampleRate: &rate, Channels: 2,
	}}}
}

func (p *fakePresets) List(context.Context, bool) ([]domain.Preset, error) {
	return p.presets, p.err
}

func (p *fakePresets) Get(_ context.Context, id string) (*domain.Preset, error) {
	for i := range p.presets {
		if p.presets[i].ID == id {
			return &p.presets[i], nil
		}
	}
	return nil, domain.NewError(domain.CodeInternal, domain.ClassLocal, "tidak ada")
}

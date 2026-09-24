package db_test

import (
	"context"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
)

// Data rilis dari katalog musik harus bertahan melewati cache, karena
// pipeline membaca metadata dari cache, bukan dari yt-dlp lagi.
func TestMediaCacheMenyimpanDataRilis(t *testing.T) {
	repo := db.NewMediaRepository(migrated(t))
	ctx := context.Background()

	withRelease := &domain.MediaInfo{
		SourceKey: "youtube:I0et_hDtfxY", Title: "Kiss the Rain", Uploader: "YIRUMA place",
		Track: "Kiss the Rain", Artist: "Yiruma", Album: "The Best - Reminiscent 10th Anniversary", ReleaseYear: 2011,
		VideoHeight: 1080,
	}
	plain := &domain.MediaInfo{SourceKey: "youtube:fJ9rUzIMcZQ", Title: "Bohemian Rhapsody", Uploader: "Queen Official"}
	for _, info := range []*domain.MediaInfo{withRelease, plain} {
		if err := repo.Upsert(ctx, info, ""); err != nil {
			t.Fatalf("Upsert() error = %v", err)
		}
	}

	got, ok, err := repo.Get(ctx, withRelease.SourceKey)
	if err != nil || !ok {
		t.Fatalf("Get() = %v, %v", ok, err)
	}
	if got.Track != withRelease.Track || got.Artist != withRelease.Artist ||
		got.Album != withRelease.Album || got.ReleaseYear != 2011 {
		t.Errorf("data rilis = %+v", got)
	}
	// Resolusi sumber menandai pilihan kualitas video di UI; pipeline dan
	// analisis ulang membacanya dari cache.
	if got.VideoHeight != 1080 {
		t.Errorf("video_height = %d, mau 1080", got.VideoHeight)
	}

	got, _, _ = repo.Get(ctx, plain.SourceKey)
	if got.Track != "" || got.Album != "" || got.ReleaseYear != 0 || got.VideoHeight != 0 {
		t.Errorf("unggahan biasa membawa data rilis: %+v", got)
	}
}

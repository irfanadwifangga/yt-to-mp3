package api_test

import (
	"context"
	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"net/http"
	"testing"
)

type playlistResolverFake struct{ urls []string }

func (f *playlistResolverFake) ResolvePlaylist(_ context.Context, url string) (*domain.Playlist, error) {
	f.urls = append(f.urls, url)
	return &domain.Playlist{Title: "Playlist", Entries: []domain.PlaylistEntry{}}, nil
}
func TestPlaylistEndpointValidatesBeforeResolving(t *testing.T) {
	fake := &playlistResolverFake{}
	h := newHarness(t)
	h.srv = api.New(api.Options{Config: config.Default(), Logger: newDiscardLogger(), Token: testToken, Port: testPort, Playlists: fake})
	invalid := h.do(t, http.MethodPost, "/api/playlists", `{"url":"https://evil.test/?list=PL_test"}`)
	if invalid.Code != 400 || len(fake.urls) != 0 {
		t.Fatalf("invalid URL reached resolver: %d", invalid.Code)
	}
	valid := h.do(t, http.MethodPost, "/api/playlists", `{"url":"https://www.youtube.com/watch?v=aaaaaaaaaaa&list=PL_test"}`)
	if valid.Code != 200 || len(fake.urls) != 1 || fake.urls[0] != "https://www.youtube.com/playlist?list=PL_test" {
		t.Fatalf("bad response: %d %s", valid.Code, valid.Body.String())
	}
}

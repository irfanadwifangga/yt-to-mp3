package api

import (
	"context"
	"encoding/json"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"net/http"
)

type PlaylistResolver interface {
	ResolvePlaylist(context.Context, string) (*domain.Playlist, error)
}

func (s *Server) handlePlaylist(w http.ResponseWriter, r *http.Request) {
	var request struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Body bukan JSON yang valid.")
		return
	}
	url, validation := domain.NormalizePlaylistURL(request.URL)
	if validation != nil {
		s.writeDomainError(w, validation)
		return
	}
	if s.playlists == nil {
		writeError(w, http.StatusServiceUnavailable, domain.CodeToolMissing, "Resolver playlist tidak tersedia.")
		return
	}
	list, err := s.playlists.ResolvePlaylist(r.Context(), url)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

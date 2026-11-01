package api

import (
	"encoding/json"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"net/http"
)

func (s *Server) handleCreatePreview(w http.ResponseWriter, r *http.Request) {
	if s.previews == nil {
		writeError(w, http.StatusServiceUnavailable, domain.CodeToolMissing, "preview lokal tidak tersedia")
		return
	}
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Body bukan JSON yang valid.")
		return
	}
	status, err := s.previews.Create(req.URL)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, status)
}
func (s *Server) handleGetPreview(w http.ResponseWriter, r *http.Request) {
	if s.previews == nil {
		writeError(w, http.StatusNotFound, domain.CodePreviewNotFound, "preview tidak tersedia")
		return
	}
	status, err := s.previews.Get(r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, status)
}
func (s *Server) handleDeletePreview(w http.ResponseWriter, r *http.Request) {
	if s.previews != nil {
		s.previews.Delete(r.PathValue("id"))
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) handlePreviewMedia(w http.ResponseWriter, r *http.Request) {
	if s.previews == nil {
		writeError(w, http.StatusNotFound, domain.CodePreviewNotFound, "preview tidak tersedia")
		return
	}
	file, err := s.previews.Open(r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `inline; filename="preview.mp4"`)
	http.ServeContent(w, r, "preview.mp4", info.ModTime(), file)
}

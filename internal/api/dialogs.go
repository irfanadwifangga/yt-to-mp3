package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// maxDialogTitle membatasi judul dialog yang dikirim klien.
const maxDialogTitle = 120

// FolderPicker membuka dialog pemilih folder native.
//
// Interface didefinisikan di sisi pemakai supaya api tidak perlu mengenal
// implementasinya.
type FolderPicker interface {
	PickFolder(ctx context.Context, title, start string) (path string, ok bool, err error)
}

type folderRequest struct {
	Title string `json:"title"`
	Start string `json:"start"`
}

type folderResponse struct {
	Path      string `json:"path,omitempty"`
	Cancelled bool   `json:"cancelled"`
}

// handlePickFolder membuka dialog pemilih folder di desktop pengguna.
//
// Endpoint ini berada di balik token dan pemeriksaan Origin seperti endpoint
// lain: tanpa itu, situs web mana pun dapat memunculkan dialog di layar
// pengguna. Judul dikirim klien karena teksnya mengikuti bahasa UI, dan
// titik awal hanya menentukan folder yang terbuka pertama kali; tidak ada
// yang dibaca atau ditulis dari path tersebut.
func (s *Server) handlePickFolder(w http.ResponseWriter, r *http.Request) {
	var req folderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Body bukan JSON yang valid.")
		return
	}

	if s.picker == nil {
		s.writeDomainError(w, domain.NewError(domain.CodeDialogUnavailable, domain.ClassLocal,
			"pemilih folder tidak dipasang"))
		return
	}

	title := []rune(req.Title)
	if len(title) > maxDialogTitle {
		title = title[:maxDialogTitle]
	}

	// Satu dialog pada satu waktu. Permintaan dari tab kedua menunggu dialog
	// pertama ditutup alih-alih menumpuk jendela di layar pengguna.
	s.dialogMu.Lock()
	defer s.dialogMu.Unlock()

	path, ok, err := s.picker.PickFolder(r.Context(), string(title), req.Start)
	if err != nil {
		s.writeDomainError(w, domain.WrapError(domain.CodeDialogUnavailable, domain.ClassLocal,
			"pemilih folder gagal dibuka", err))
		return
	}
	writeJSON(w, http.StatusOK, folderResponse{Path: path, Cancelled: !ok})
}

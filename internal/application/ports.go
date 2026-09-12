// Package application memuat use case dan port (interface) yang
// diimplementasikan oleh layer infrastructure.
//
// Port didefinisikan di sini, bukan di infrastructure, supaya api dapat
// bergantung pada kontrak tanpa pernah mengimpor implementasinya. Lihat
// docs architecture "Prinsip dan aturan dependensi".
package application

import (
	"context"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// ToolStatus adalah hasil discovery satu tool eksternal.
type ToolStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Source    string `json:"source,omitempty"`
	Pinned    string `json:"pinned_version,omitempty"`

	// Path sengaja tidak punya tag JSON: path filesystem tidak pernah
	// menyeberang batas API.
	Path string `json:"-"`
}

// ToolManager menemukan dan memasang yt-dlp serta FFmpeg.
type ToolManager interface {
	StatusAll(ctx context.Context) map[string]ToolStatus
	Install(ctx context.Context, name string) error
}

// MediaResolver mengambil metadata sumber.
type MediaResolver interface {
	Resolve(ctx context.Context, sourceKey string) (*domain.MediaInfo, error)
}

// Package application memuat use case dan port (interface) yang
// diimplementasikan oleh layer infrastructure.
//
// Port didefinisikan di sini, bukan di infrastructure, supaya api dapat
// bergantung pada kontrak tanpa pernah mengimpor implementasinya. Lihat
// docs architecture "Prinsip dan aturan dependensi".
package application

import (
	"context"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// ToolStatus adalah hasil discovery satu tool eksternal.
type ToolStatus struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Source    string `json:"source,omitempty"`
	Pinned    string `json:"pinned_version,omitempty"`

	// Latest adalah versi rilis terbaru dari cek pembaruan terakhir.
	Latest          string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`

	// Updatable berarti tool dapat diperbarui ke rilis terbaru dari
	// aplikasi dengan satu klik. Selain itu, pembaruannya mengikuti rilis
	// aplikasi atau package manager.
	Updatable bool `json:"updatable"`

	// Path sengaja tidak punya tag JSON: path filesystem tidak pernah
	// menyeberang batas API.
	Path string `json:"-"`
}

// AppUpdate adalah status pembaruan aplikasi ini sendiri.
type AppUpdate struct {
	Current string `json:"current"`
	// Latest adalah versi rilis terbaru dari cek pembaruan terakhir, kosong
	// bila belum pernah berhasil dicek.
	Latest          string `json:"latest,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url,omitempty"`
}

// Fase instalasi tool yang dilaporkan ToolProgress.
const (
	ToolPhaseDownloading = "downloading"
	ToolPhaseExtracting  = "extracting"
)

// ToolProgress adalah kemajuan instalasi atau pembaruan satu tool.
type ToolProgress struct {
	Phase string `json:"phase"`
	// Step adalah unduhan ke berapa dari Steps; FFmpeg untuk Linux dan
	// macOS terdiri dari dua arsip terpisah.
	Step      int   `json:"step"`
	Steps     int   `json:"steps"`
	DoneBytes int64 `json:"done_bytes"`
	// TotalBytes bernilai 0 bila server tidak menyebutkan ukuran.
	TotalBytes int64 `json:"total_bytes"`
}

// ToolManager menemukan, memasang, dan memperbarui yt-dlp serta FFmpeg.
type ToolManager interface {
	StatusAll(ctx context.Context) map[string]ToolStatus
	Install(ctx context.Context, name string) error
	CheckUpdates(ctx context.Context) error
	Update(ctx context.Context, name string) error
	CheckedAt() *time.Time
	// Progress mengembalikan kemajuan instalasi yang sedang berjalan per
	// nama tool. Tidak pernah nil.
	Progress() map[string]ToolProgress
	// AppUpdate mengembalikan status pembaruan aplikasi dari cek terakhir.
	AppUpdate() AppUpdate
}

// MediaResolver mengambil metadata sumber.
type MediaResolver interface {
	Resolve(ctx context.Context, sourceKey string) (*domain.MediaInfo, error)
}

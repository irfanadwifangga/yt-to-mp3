package api

import (
	"net/http"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
)

// pingResponse sengaja minim: endpoint ini publik (tanpa token) karena
// dipakai proses kedua untuk mendeteksi instance yang sudah berjalan.
type pingResponse struct {
	App     string `json:"app"`
	Version string `json:"version"`
}

func (s *Server) handlePing(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, pingResponse{
		App:     version.AppName,
		Version: version.Version,
	})
}

type toolStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Path      string `json:"path"`
}

type queueStatus struct {
	Active   int `json:"active"`
	Queued   int `json:"queued"`
	Capacity int `json:"capacity"`
}

type healthResponse struct {
	App           string                `json:"app"`
	Version       string                `json:"version"`
	Commit        string                `json:"commit"`
	Status        string                `json:"status"`
	UptimeSeconds int64                 `json:"uptime_seconds"`
	SPABuilt      bool                  `json:"spa_built"`
	OutputDir     string                `json:"output_dir"`
	Tools         map[string]toolStatus `json:"tools"`
	Queue         queueStatus           `json:"queue"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// Tool discovery menyusul pada tahap 2 roadmap; sampai saat itu status
	// dilaporkan apa adanya, bukan dipalsukan jadi tersedia.
	writeJSON(w, http.StatusOK, healthResponse{
		App:           version.AppName,
		Version:       version.Version,
		Commit:        version.Commit,
		Status:        "ok",
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		SPABuilt:      s.spaBuilt,
		OutputDir:     s.cfg.OutputDir,
		Tools: map[string]toolStatus{
			"yt_dlp": {Available: false},
			"ffmpeg": {Available: false},
		},
		Queue: queueStatus{Active: 0, Queued: 0, Capacity: s.cfg.MaxQueueDepth},
	})
}

// handleShutdown adalah tombol Quit di SPA. Tanpa tray icon, inilah cara
// pengguna menghentikan aplikasi. Lihat ADR-021.
func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "shutting_down"})
	s.requestShutdown()
}

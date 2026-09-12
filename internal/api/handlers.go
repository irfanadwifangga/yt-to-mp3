package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
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

type healthResponse struct {
	App           string                            `json:"app"`
	Version       string                            `json:"version"`
	Commit        string                            `json:"commit"`
	Status        string                            `json:"status"`
	UptimeSeconds int64                             `json:"uptime_seconds"`
	SPABuilt      bool                              `json:"spa_built"`
	OutputDir     string                            `json:"output_dir"`
	Tools         map[string]application.ToolStatus `json:"tools"`
	Queue         application.QueueStatus           `json:"queue"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	// Antrean masih nol sampai job engine hadir pada tahap 4 roadmap.
	writeJSON(w, http.StatusOK, healthResponse{
		App:           version.AppName,
		Version:       version.Version,
		Commit:        version.Commit,
		Status:        "ok",
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		SPABuilt:      s.spaBuilt,
		OutputDir:     s.cfg.OutputDir,
		Tools:         s.tools.StatusAll(r.Context()),
		Queue:         s.jobs.Queue(r.Context()),
	})
}

// handleShutdown adalah tombol Quit di SPA. Tanpa tray icon, inilah cara
// pengguna menghentikan aplikasi. Lihat ADR-021.
func (s *Server) handleShutdown(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "shutting_down"})
	s.requestShutdown()
}

type toolsResponse struct {
	Tools map[string]application.ToolStatus `json:"tools"`
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, toolsResponse{Tools: s.tools.StatusAll(r.Context())})
}

type installRequest struct {
	Name string `json:"name"`
}

// handleToolInstall memasang satu tool.
//
// Instalasi berjalan sinkron dan bisa memakan waktu beberapa menit; progres
// terunduh baru bisa distream setelah SSE hub dipakai pada tahap 4.
func (s *Server) handleToolInstall(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Body bukan JSON yang valid.")
		return
	}

	switch req.Name {
	case "yt-dlp", "ffmpeg":
	default:
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Nama tool tidak dikenal.")
		return
	}

	if err := s.tools.Install(r.Context(), req.Name); err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toolsResponse{Tools: s.tools.StatusAll(r.Context())})
}

type metadataRequest struct {
	URL string `json:"url"`
}

type metadataResponse struct {
	SourceKey    string `json:"source_key"`
	SourceURL    string `json:"source_url"`
	Title        string `json:"title"`
	Uploader     string `json:"uploader"`
	DurationMS   int64  `json:"duration_ms"`
	ThumbnailURL string `json:"thumbnail_url"`
	SourceCodec  string `json:"source_codec"`
	SampleRate   int    `json:"sample_rate"`
}

// handleMetadata menganalisis URL tanpa mengunduh apa pun.
func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	var req metadataRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Body bukan JSON yang valid.")
		return
	}

	// Normalisasi, cache, dan pemanggilan tool ditangani use case; handler
	// hanya menerjemahkan bentuk HTTP-nya.
	info, err := s.metadata.Analyze(r.Context(), req.URL)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, metadataResponse{
		SourceKey:    info.SourceKey,
		SourceURL:    info.SourceURL,
		Title:        info.Title,
		Uploader:     info.Uploader,
		DurationMS:   info.DurationMS,
		ThumbnailURL: info.ThumbnailURL,
		SourceCodec:  info.SourceCodec,
		SampleRate:   info.SampleRate,
	})
}

type presetsResponse struct {
	Presets []domain.Preset `json:"presets"`
}

// handlePresets memasok daftar preset ke UI. SPA tidak boleh meng-hardcode
// preset karena definisinya adalah product contract yang hidup di database.
func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	presets, err := s.presets.List(r.Context(), false)
	if err != nil {
		s.log.Error("baca preset gagal", "error", err)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Tidak dapat membaca daftar preset.")
		return
	}
	writeJSON(w, http.StatusOK, presetsResponse{Presets: presets})
}

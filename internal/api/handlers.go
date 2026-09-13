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
	writeJSON(w, http.StatusOK, healthResponse{
		App:           version.AppName,
		Version:       version.Version,
		Commit:        version.Commit,
		Status:        "ok",
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		SPABuilt:      s.spaBuilt,
		OutputDir:     s.currentOutputDir(),
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

	// CheckedAt adalah waktu cek pembaruan terakhir yang berhasil.
	CheckedAt *time.Time `json:"checked_at,omitempty"`
}

func (s *Server) toolsView(r *http.Request) toolsResponse {
	return toolsResponse{Tools: s.tools.StatusAll(r.Context()), CheckedAt: s.tools.CheckedAt()}
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.toolsView(r))
}

// handleToolCheck menanyakan versi terbaru seluruh tool sekarang juga.
// Tidak ada yang dipasang; hasilnya hanya menandai tool yang tertinggal.
func (s *Server) handleToolCheck(w http.ResponseWriter, r *http.Request) {
	if err := s.tools.CheckUpdates(r.Context()); err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toolsView(r))
}

// handleToolUpdate memperbarui yt-dlp ke rilis terbaru atas permintaan
// pengguna. FFmpeg sengaja tidak diterima; versinya mengikuti manifest yang
// di-pin di rilis aplikasi.
func (s *Server) handleToolUpdate(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Body bukan JSON yang valid.")
		return
	}
	if req.Name != "yt-dlp" {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Hanya yt-dlp yang dapat diperbarui dari aplikasi.")
		return
	}

	if err := s.tools.Update(r.Context(), req.Name); err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toolsView(r))
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
	writeJSON(w, http.StatusOK, s.toolsView(r))
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

type settingsResponse struct {
	Settings []application.SettingView `json:"settings"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	views, err := s.settings.List(r.Context())
	if err != nil {
		s.log.Error("baca setelan gagal", "error", err)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Tidak dapat membaca setelan.")
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{Settings: views})
}

// handleUpdateSettings menyimpan perubahan setelan.
//
// Hanya kunci yang dikirim yang diubah, sehingga klien tidak perlu
// mengirim ulang seluruh konfigurasi dan tidak berisiko menimpa setelan
// yang baru saja diubah dari tempat lain.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req map[string]string
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Body bukan JSON yang valid.")
		return
	}

	if err := s.settings.Update(r.Context(), req); err != nil {
		s.writeDomainError(w, err)
		return
	}

	views, err := s.settings.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Setelan tersimpan tetapi tidak dapat dibaca ulang.")
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse{Settings: views})
}

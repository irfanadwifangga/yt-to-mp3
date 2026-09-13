package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// heartbeatInterval menjaga koneksi SSE tetap hidup melewati idle timeout
// proxy maupun browser.
const heartbeatInterval = 15 * time.Second

// jobView adalah bentuk job yang dikirim ke klien.
//
// Tidak ada path filesystem di sini; berkas hanya diakses lewat id.
type jobView struct {
	ID           string           `json:"id"`
	SourceURL    string           `json:"source_url"`
	SourceKey    string           `json:"source_key"`
	Title        string           `json:"title"`
	Status       domain.JobStatus `json:"status"`
	PresetID     string           `json:"preset_id"`
	FilenameMode string           `json:"filename_mode"`
	Progress     *float64         `json:"progress"`
	Phase        string           `json:"phase,omitempty"`
	AttemptCount int              `json:"attempt_count"`
	ErrorCode    domain.ErrorCode `json:"error_code,omitempty"`

	// RetryAt terisi selama job menunggu jeda auto-retry.
	RetryAt *time.Time `json:"retry_at,omitempty"`

	// FileID terisi hanya bila berkas hasilnya masih ada di disk, sehingga
	// UI dapat menyembunyikan tombol unduh yang pasti gagal.
	FileID string `json:"file_id,omitempty"`

	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

func toJobView(j *domain.Job) jobView {
	return jobView{
		ID:           j.ID,
		SourceURL:    j.SourceURL,
		SourceKey:    j.SourceKey,
		Title:        j.Title,
		Status:       j.Status,
		PresetID:     j.PresetID,
		FilenameMode: string(j.FilenameMode),
		Progress:     j.Progress,
		Phase:        j.Phase,
		AttemptCount: j.AttemptCount,
		ErrorCode:    j.ErrorCode,
		RetryAt:      j.RetryAt,
		CreatedAt:    j.CreatedAt,
		StartedAt:    j.StartedAt,
		FinishedAt:   j.FinishedAt,
	}
}

type createJobRequest struct {
	URL          string `json:"url"`
	PresetID     string `json:"preset_id"`
	FilenameMode string `json:"filename_mode"`
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Body bukan JSON yang valid.")
		return
	}

	result, err := s.jobs.Create(r.Context(), application.CreateRequest{
		URL:          req.URL,
		PresetID:     req.PresetID,
		FilenameMode: domain.FilenameMode(req.FilenameMode),
	})
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toJobView(result.Job))
}

type listJobsResponse struct {
	Jobs       []jobView `json:"jobs"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	q := application.JobListQuery{Cursor: r.URL.Query().Get("cursor")}

	// Selain status tunggal, status menerima dua kelompok: active untuk
	// antrean dan finished untuk riwayat. UI butuh keduanya terpisah supaya
	// riwayat panjang tidak mendesak job aktif keluar dari halaman.
	switch status := r.URL.Query().Get("status"); status {
	case string(application.ScopeActive):
		q.Scope = application.ScopeActive
	case string(application.ScopeFinished):
		q.Scope = application.ScopeFinished
	default:
		q.Status = domain.JobStatus(status)
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, CodeBadRequest, "Parameter limit tidak valid.")
			return
		}
		q.Limit = n
	}
	if q.Status != "" && !q.Status.Valid() {
		writeError(w, http.StatusBadRequest, CodeBadRequest, "Parameter status tidak dikenal.")
		return
	}

	jobs, next, err := s.jobs.List(r.Context(), q)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	// Id berkas diambil sekali untuk seluruh halaman, bukan satu query per
	// baris.
	ids := make([]string, 0, len(jobs))
	for _, j := range jobs {
		ids = append(ids, j.ID)
	}
	fileIDs, err := s.files.IDsByJobs(r.Context(), ids)
	if err != nil {
		s.log.Warn("baca id berkas gagal", "error", err)
		fileIDs = nil
	}

	views := make([]jobView, 0, len(jobs))
	for _, j := range jobs {
		v := toJobView(j)
		v.FileID = fileIDs[j.ID]
		views = append(views, v)
	}
	writeJSON(w, http.StatusOK, listJobsResponse{Jobs: views, NextCursor: next})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	view := toJobView(job)
	if fileIDs, err := s.files.IDsByJobs(r.Context(), []string{job.ID}); err == nil {
		view.FileID = fileIDs[job.ID]
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	if err := s.canceller.Cancel(r.Context(), r.PathValue("id")); err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancelling"})
}

func (s *Server) handleRetryJob(w http.ResponseWriter, r *http.Request) {
	job, err := s.jobs.Retry(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toJobView(job))
}

// handleDeleteJob menghapus job dari riwayat. delete_file=true ikut
// menghapus berkas hasilnya; path berkas tetap dibaca dari database, bukan
// dari klien.
func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	deleteFile := r.URL.Query().Get("delete_file") == "true"
	if err := s.jobs.Delete(r.Context(), r.PathValue("id"), deleteFile); err != nil {
		s.writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleJobEvents mengalirkan progress lewat Server-Sent Events.
//
// Job yang sudah terminal tidak menggantungkan koneksi: pelanggan menerima
// histori beserta state akhirnya, lalu stream ditutup.
func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	job, err := s.jobs.Get(r.Context(), id)
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Streaming tidak didukung.")
		return
	}

	events, unsubscribe, err := s.hub.Subscribe(r.Context(), id, lastEventID(r), job.Status.IsTerminal())
	if err != nil {
		s.writeDomainError(w, err)
		return
	}
	defer unsubscribe()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-s.streamsDone:
			// Server sedang berhenti. Klien yang masih hidup akan menyambung
			// ulang dan jatuh ke polling bila server memang sudah tiada.
			return

		case ev, open := <-events:
			// Kanal tertutup berarti hub sudah selesai dengan job ini,
			// misalnya karena statusnya sudah terminal saat berlangganan.
			if !open {
				return
			}
			if err := writeSSE(w, ev); err != nil {
				s.log.Debug("tulis SSE gagal", "job", id, "error", err)
				return
			}
			flusher.Flush()
			if ev.Type == application.StreamDone {
				return
			}

		case <-ticker.C:
			// Komentar SSE; diabaikan klien tetapi menjaga koneksi hidup.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// lastEventID membaca posisi resume dari header standar SSE, dengan query
// sebagai cadangan untuk klien yang tidak dapat menyetel header.
func lastEventID(r *http.Request) int64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		raw = r.URL.Query().Get("last_event_id")
	}
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// writeSSE menulis satu event dalam format text/event-stream.
//
// Field id hanya ditulis untuk event terpersist. Event progress sengaja
// tanpa id supaya tidak menggeser Last-Event-ID klien; posisi resume harus
// selalu menunjuk event yang benar-benar ada di job_events (ADR-023).
func writeSSE(w http.ResponseWriter, ev application.StreamEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if ev.Seq > 0 {
		if _, err := fmt.Fprintf(w, "id: %d\n", ev.Seq); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
	return err
}

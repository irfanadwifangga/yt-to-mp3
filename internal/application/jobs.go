package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// JobScope mengelompokkan status untuk daftar di UI.
type JobScope string

const (
	ScopeAll      JobScope = ""
	ScopeActive   JobScope = "active"   // queued sampai cancelling
	ScopeFinished JobScope = "finished" // completed, failed, cancelled
)

// JobListQuery adalah parameter pagination history.
type JobListQuery struct {
	Status domain.JobStatus // kosong berarti seluruh status
	Scope  JobScope
	Limit  int
	Cursor string
}

// JobRepository adalah port penyimpanan job.
type JobRepository interface {
	Create(ctx context.Context, j *domain.Job) error
	Get(ctx context.Context, id string) (*domain.Job, error)
	List(ctx context.Context, q JobListQuery) ([]*domain.Job, string, error)
	Transition(ctx context.Context, id string, from, to domain.JobStatus, ev domain.Event) error
	Fail(ctx context.Context, id string, from domain.JobStatus, code domain.ErrorCode, detail string) error
	// Requeue mengembalikan job yang gagal sementara ke antrean, ditahan
	// sampai retryAt.
	Requeue(ctx context.Context, id string, from domain.JobStatus, code domain.ErrorCode, detail string, retryAt time.Time) error
	ClaimNextQueued(ctx context.Context) (*domain.Job, error)
	SweepNonTerminal(ctx context.Context) (int, error)
	Delete(ctx context.Context, id string) error
	Events(ctx context.Context, jobID string, afterSeq int64) ([]domain.Event, error)
	CountActive(ctx context.Context) (active int, queued int, err error)
}

// StreamEventType adalah jenis event yang dikirim lewat SSE.
//
// Berbeda dari domain.EventType, jenis di sini memuat progress yang hidup di
// memori saja dan tidak pernah dipersist. Lihat ADR-023.
type StreamEventType string

const (
	StreamState    StreamEventType = "state"
	StreamProgress StreamEventType = "progress"
	StreamLog      StreamEventType = "log"
	StreamDone     StreamEventType = "done"
	StreamError    StreamEventType = "error"
)

// Persisted melaporkan apakah event jenis ini disimpan ke job_events.
func (t StreamEventType) Persisted() bool {
	switch t {
	case StreamState, StreamDone, StreamError:
		return true
	default:
		return false
	}
}

// StreamEvent adalah satu event yang disiarkan ke pelanggan SSE.
type StreamEvent struct {
	JobID   string           `json:"-"`
	Seq     int64            `json:"-"`
	Type    StreamEventType  `json:"-"`
	Status  domain.JobStatus `json:"status,omitempty"`
	Phase   string           `json:"phase,omitempty"`
	Percent *float64         `json:"percent,omitempty"`
	Code    domain.ErrorCode `json:"code,omitempty"`
	Message string           `json:"message,omitempty"`
}

// EventPublisher menyiarkan event ke pelanggan yang sedang terhubung.
type EventPublisher interface {
	Publish(ev StreamEvent)
}

// JobRunner menjalankan satu job dari resolving sampai berkas final.
//
// Implementasi sungguhannya adalah pipeline yt-dlp dan FFmpeg. Port ini
// memisahkan mesin antrean dari isi pekerjaannya, sehingga scheduler dapat
// diuji tanpa menyentuh jaringan maupun proses eksternal.
type JobRunner interface {
	Run(ctx context.Context, job *domain.Job) error
}

// NewJobID membuat identitas job baru.
func NewJobID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("buat id job: %w", err)
	}
	return "job_" + hex.EncodeToString(buf), nil
}

// JobFiles mencari berkas hasil sebuah job.
type JobFiles interface {
	GetByJob(ctx context.Context, jobID string) (*domain.File, error)
}

// JobService adalah use case pembuatan dan pengelolaan job.
type JobService struct {
	repo     JobRepository
	presets  PresetLister
	cache    MediaCache
	resolver MediaResolver
	files    JobFiles
	events   EventPublisher
	notify   chan<- struct{}
	log      *slog.Logger

	// live dibaca setiap permintaan, bukan sekali saat startup, sehingga
	// perubahan preset default dan batas antrean berlaku tanpa restart.
	live func() LiveSettings
}

// NewJobService membuat use case job.
func NewJobService(
	repo JobRepository,
	presets PresetLister,
	cache MediaCache,
	resolver MediaResolver,
	files JobFiles,
	events EventPublisher,
	notify chan<- struct{},
	live func() LiveSettings,
	log *slog.Logger,
) *JobService {
	return &JobService{
		repo: repo, presets: presets, cache: cache, resolver: resolver,
		files: files, events: events, notify: notify, live: live, log: log,
	}
}

// CreateRequest adalah permintaan pembuatan job.
type CreateRequest struct {
	URL          string
	PresetID     string
	FilenameMode domain.FilenameMode
}

// CreateResult memuat job baru beserta petunjuk berkas yang sudah ada.
type CreateResult struct {
	Job *domain.Job

	// ExistingJobID terisi bila sumber dan preset yang sama pernah selesai.
	// Job baru tetap dibuat; UI yang memutuskan menawarkan berkas lama.
	ExistingJobID string
}

// Create memvalidasi permintaan lalu mengantrekan job baru.
func (s *JobService) Create(ctx context.Context, req CreateRequest) (*CreateResult, error) {
	sourceKey, derr := domain.NormalizeURL(req.URL)
	if derr != nil {
		return nil, derr
	}

	settings := s.live()

	presetID := req.PresetID
	if presetID == "" {
		presetID = settings.DefaultPresetID
	}
	preset, err := s.presets.Get(ctx, presetID)
	if err != nil {
		return nil, domain.NewError(domain.CodeInternal, domain.ClassLocal,
			fmt.Sprintf("preset %s tidak dikenal", presetID))
	}
	if preset.Deprecated {
		return nil, domain.NewError(domain.CodeInternal, domain.ClassLocal,
			fmt.Sprintf("preset %s sudah tidak ditawarkan", presetID))
	}

	mode := req.FilenameMode
	if mode == "" {
		mode = settings.FilenameMode
	}
	if !mode.Valid() {
		return nil, domain.NewError(domain.CodeInternal, domain.ClassLocal,
			fmt.Sprintf("filename_mode %q tidak dikenal", mode))
	}

	// Antrean dibatasi di sini, bukan di worker, supaya penolakan terjadi
	// saat request dan pemanggil langsung tahu.
	_, queued, err := s.repo.CountActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("hitung antrean: %w", err)
	}
	if queued >= settings.MaxQueueDepth {
		return nil, domain.NewError(domain.CodeQueueFull, domain.ClassLocal,
			fmt.Sprintf("antrean penuh (%d)", queued))
	}

	title := s.knownTitle(ctx, sourceKey)

	id, err := NewJobID()
	if err != nil {
		return nil, err
	}
	job := &domain.Job{
		ID:           id,
		SourceURL:    req.URL,
		SourceKey:    sourceKey,
		Title:        title,
		Status:       domain.StatusQueued,
		PresetID:     presetID,
		FilenameMode: mode,
		CreatedAt:    time.Now().UTC(),
	}

	if err := s.repo.Create(ctx, job); err != nil {
		return nil, err
	}

	s.wake()
	return &CreateResult{Job: job}, nil
}

// knownTitle mengambil judul dari cache bila ada, supaya antrean tidak
// menampilkan baris tanpa nama sebelum resolving berjalan.
func (s *JobService) knownTitle(ctx context.Context, sourceKey string) string {
	info, ok, err := s.cache.Get(ctx, sourceKey)
	if err != nil || !ok {
		return ""
	}
	return info.Title
}

// wake memberi tahu scheduler tanpa memblokir; sinyal yang menumpuk tidak
// berguna karena scheduler akan menguras antrean sampai habis.
func (s *JobService) wake() {
	if s.notify == nil {
		return
	}
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// Get mengambil satu job.
func (s *JobService) Get(ctx context.Context, id string) (*domain.Job, error) {
	return s.repo.Get(ctx, id)
}

// List mengembalikan satu halaman history.
func (s *JobService) List(ctx context.Context, q JobListQuery) ([]*domain.Job, string, error) {
	return s.repo.List(ctx, q)
}

// Delete menghapus job dari history, dan bila diminta, berkas hasilnya.
//
// Berkas dihapus lebih dulu. Bila penghapusan berkas gagal, job dibiarkan
// supaya berkas yang tertinggal tetap bisa ditemukan dan dihapus lagi dari
// riwayat; urutan sebaliknya meninggalkan berkas yatim tanpa jalan kembali
// dari UI.
func (s *JobService) Delete(ctx context.Context, id string, deleteFile bool) error {
	job, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Status.IsActive() {
		return domain.NewError(domain.CodeInternal, domain.ClassLocal,
			"job yang masih berjalan harus dibatalkan lebih dulu")
	}
	if deleteFile {
		if err := s.removeFile(ctx, id); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id)
}

// removeFile menghapus berkas hasil sebuah job dari disk.
//
// Path hanya dibaca dari database, tidak pernah dari klien. Job tanpa
// berkas, atau berkas yang sudah hilang, bukan kegagalan: yang diminta
// pengguna memang agar berkas itu tidak ada.
func (s *JobService) removeFile(ctx context.Context, jobID string) error {
	if s.files == nil {
		return nil
	}
	file, err := s.files.GetByJob(ctx, jobID)
	var derr *domain.Error
	if errors.As(err, &derr) && derr.Code == domain.CodeJobNotFound {
		return nil
	}
	if err != nil {
		return err
	}

	if err := os.Remove(file.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return domain.WrapError(domain.CodeOutputWriteFailed, domain.ClassLocal,
			"hapus berkas hasil", err)
	}
	s.log.Info("berkas hasil dihapus", "job", jobID, "berkas", file.Filename)
	return nil
}

// Retry mengantrekan ulang job yang gagal atau dibatalkan.
func (s *JobService) Retry(ctx context.Context, id string) (*domain.Job, error) {
	job, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Status.IsActive() {
		return nil, domain.NewError(domain.CodeInternal, domain.ClassLocal,
			"job masih berjalan")
	}
	if job.Status == domain.StatusCompleted {
		return nil, domain.NewError(domain.CodeInternal, domain.ClassLocal,
			"job sudah selesai")
	}

	// Kelas permanen tidak akan pernah berhasil diulang, jadi ditolak di
	// sini alih-alih membuang satu putaran pipeline.
	if permanentCodes[job.ErrorCode] {
		return nil, domain.NewError(job.ErrorCode, domain.ClassPermanent,
			"kegagalan ini tidak dapat diulang")
	}

	newID, err := NewJobID()
	if err != nil {
		return nil, err
	}
	retry := &domain.Job{
		ID:           newID,
		SourceURL:    job.SourceURL,
		SourceKey:    job.SourceKey,
		Title:        job.Title,
		Status:       domain.StatusQueued,
		PresetID:     job.PresetID,
		FilenameMode: job.FilenameMode,
		// Retry manual adalah keputusan pengguna, jadi job baru mendapat
		// jatah auto-retry penuh alih-alih mewarisi jatah yang sudah habis.
		AttemptCount: 0,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.repo.Create(ctx, retry); err != nil {
		return nil, err
	}

	s.wake()
	return retry, nil
}

// permanentCodes adalah kegagalan yang tidak pernah berubah hasilnya bila
// diulang. Lihat docs planning "Klasifikasi error dan kebijakan retry".
var permanentCodes = map[domain.ErrorCode]bool{
	domain.CodeVideoPrivate:     true,
	domain.CodeVideoUnavailable: true,
	domain.CodeGeoBlocked:       true,
	domain.CodeAgeRestricted:    true,
	domain.CodeLiveNotSupported: true,
	domain.CodeUnsupportedURL:   true,
	domain.CodeInvalidURL:       true,
}

// QueueStatus adalah ringkasan antrean untuk health.
type QueueStatus struct {
	Active   int `json:"active"`
	Queued   int `json:"queued"`
	Capacity int `json:"capacity"`
}

// Queue mengembalikan ringkasan antrean saat ini.
func (s *JobService) Queue(ctx context.Context) QueueStatus {
	active, queued, err := s.repo.CountActive(ctx)
	if err != nil {
		s.log.Warn("hitung antrean gagal", "error", err)
	}
	return QueueStatus{Active: active, Queued: queued, Capacity: s.live().MaxQueueDepth}
}

// PayloadJSON menyusun isi kolom job_events untuk event ini.
//
// Dipersist sebagai JSON dengan bentuk yang sama seperti event live,
// sehingga pelanggan yang menyambung ulang menerima struktur identik dengan
// yang diterima pelanggan yang tidak pernah terputus.
func (e StreamEvent) PayloadJSON() string {
	raw, err := json.Marshal(e)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// ParseEventPayload membaca kembali payload yang dipersist.
//
// Payload lama yang berupa teks biasa tetap diterima dan dipetakan ke
// Message, supaya database yang sudah terisi tidak perlu dimigrasi.
func ParseEventPayload(payload string) StreamEvent {
	var ev StreamEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return StreamEvent{Message: payload}
	}
	return ev
}

// FileStore membaca berkas hasil.
type FileStore interface {
	Get(ctx context.Context, id string) (*domain.File, error)
	IDsByJobs(ctx context.Context, jobIDs []string) (map[string]string, error)
	MarkMissing(ctx context.Context, id string) error
}

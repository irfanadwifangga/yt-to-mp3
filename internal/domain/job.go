package domain

import (
	"fmt"
	"time"
)

// JobStatus adalah state sebuah job konversi.
type JobStatus string

const (
	StatusQueued      JobStatus = "queued"
	StatusResolving   JobStatus = "resolving"
	StatusDownloading JobStatus = "downloading"
	StatusConverting  JobStatus = "converting"
	StatusVerifying   JobStatus = "verifying"
	StatusCompleted   JobStatus = "completed"
	StatusFailed      JobStatus = "failed"
	StatusCancelling  JobStatus = "cancelling"
	StatusCancelled   JobStatus = "cancelled"
)

// allowedTransitions adalah satu-satunya sumber kebenaran perpindahan state.
//
// Tidak ada tempat lain di codebase yang boleh menulis kolom status tanpa
// melewati CanTransition. Lihat docs architecture "Invarian".
var allowedTransitions = map[JobStatus][]JobStatus{
	// Job yang masih antre belum punya proses OS, jadi boleh langsung
	// dibatalkan tanpa melewati cancelling.
	StatusQueued: {StatusResolving, StatusCancelled, StatusFailed},

	// Kembali ke queued hanya untuk auto-retry setelah kegagalan sementara.
	// Pipeline bersifat idempoten (ADR-008): setiap percobaan menulis ke
	// temp baru dan hanya menyentuh keluaran final saat commit, jadi
	// mengulang dari awal aman di fase mana pun.
	StatusResolving:   {StatusDownloading, StatusCancelling, StatusFailed, StatusQueued},
	StatusDownloading: {StatusConverting, StatusCancelling, StatusFailed, StatusQueued},
	StatusConverting:  {StatusVerifying, StatusCancelling, StatusFailed, StatusQueued},
	StatusVerifying:   {StatusCompleted, StatusCancelling, StatusFailed, StatusQueued},
	StatusCancelling:  {StatusCancelled, StatusFailed},

	// completed, failed, dan cancelled bersifat terminal.
	StatusCompleted: nil,
	StatusFailed:    nil,
	StatusCancelled: nil,
}

// AllStatuses mengembalikan seluruh status yang dikenal.
func AllStatuses() []JobStatus {
	return []JobStatus{
		StatusQueued, StatusResolving, StatusDownloading, StatusConverting,
		StatusVerifying, StatusCompleted, StatusFailed, StatusCancelling,
		StatusCancelled,
	}
}

// Valid melaporkan apakah status dikenali.
func (s JobStatus) Valid() bool {
	_, ok := allowedTransitions[s]
	return ok
}

// IsTerminal melaporkan apakah job sudah selesai dan tidak akan berubah lagi.
func (s JobStatus) IsTerminal() bool {
	return len(allowedTransitions[s]) == 0 && s.Valid()
}

// IsActive melaporkan apakah job sedang memegang proses atau antrean.
func (s JobStatus) IsActive() bool {
	return s.Valid() && !s.IsTerminal()
}

// CanTransition melaporkan apakah perpindahan state diizinkan.
func CanTransition(from, to JobStatus) bool {
	for _, candidate := range allowedTransitions[from] {
		if candidate == to {
			return true
		}
	}
	return false
}

// ErrInvalidTransition menjelaskan perpindahan state yang ditolak.
func ErrInvalidTransition(from, to JobStatus) *Error {
	return NewError(CodeInternal, ClassLocal,
		fmt.Sprintf("transisi %s -> %s tidak diizinkan", from, to))
}

// FilenameMode menentukan bentuk nama berkas hasil.
type FilenameMode string

const (
	FilenameTitle         FilenameMode = "title"
	FilenameTitleUploader FilenameMode = "title-uploader"
	FilenameUploaderTitle FilenameMode = "uploader-title"
	FilenameID            FilenameMode = "id"
)

// Valid melaporkan apakah mode dikenali.
func (m FilenameMode) Valid() bool {
	switch m {
	case FilenameTitle, FilenameTitleUploader, FilenameUploaderTitle, FilenameID:
		return true
	default:
		return false
	}
}

// Job adalah satu permintaan konversi.
type Job struct {
	ID           string
	SourceURL    string
	SourceKey    string
	Title        string
	Status       JobStatus
	PresetID     string
	FilenameMode FilenameMode

	// Progress bernilai nil ketika indeterminate, yaitu ukuran total atau
	// durasi belum diketahui. Itu nilai yang sah, bukan data hilang.
	Progress *float64
	Phase    string

	// AttemptCount adalah jumlah auto-retry yang sudah dijalankan untuk job
	// ini. Retry manual membuat job baru dengan jatah penuh.
	AttemptCount int
	ErrorCode    ErrorCode
	ErrorMessage string

	// RetryAt terisi selama job menunggu jeda auto-retry.
	RetryAt *time.Time

	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// EventType adalah jenis event yang dipersist ke job_events.
//
// Event progress sengaja tidak termasuk: pada 4 event per detik,
// mempersistkannya menghasilkan ribuan baris per job. Lihat ADR-023.
type EventType string

const (
	EventState EventType = "state"
	EventError EventType = "error"
	EventDone  EventType = "done"
)

// Valid melaporkan apakah jenis event boleh dipersist.
func (t EventType) Valid() bool {
	switch t {
	case EventState, EventError, EventDone:
		return true
	default:
		return false
	}
}

// Event adalah satu baris job_events.
type Event struct {
	JobID     string
	Seq       int64
	Type      EventType
	Payload   string
	CreatedAt time.Time
}

// Preset adalah definisi format keluaran. Tabelnya append-only karena
// preset_id tersimpan permanen di history. Lihat ADR-028.
type Preset struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Format      string `json:"format"`
	Codec       string `json:"codec"`
	Mode        string `json:"mode"`
	BitrateKbps *int   `json:"bitrate_kbps,omitempty"`
	VBRQuality  *int   `json:"vbr_quality,omitempty"`
	SampleRate  *int   `json:"sample_rate,omitempty"`
	Channels    int    `json:"channels"`
	ExtraArgs   string `json:"-"`
	SortOrder   int    `json:"-"`
	Deprecated  bool   `json:"deprecated"`
}

// File adalah berkas hasil akhir sebuah job.
type File struct {
	ID        string
	JobID     string
	Path      string
	Filename  string
	MIME      string
	SizeBytes int64
	SHA256    string
	Missing   bool
	CreatedAt time.Time
}

package application

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// Kebijakan retensi. Lihat data-model "Retensi" dan architecture
// "Housekeeping".
const (
	HousekeepingInterval = time.Hour
	EventRetention       = 30 * 24 * time.Hour
	TempMaxAge           = 24 * time.Hour
)

// EventPruner membuang event job terminal yang sudah lama.
type EventPruner interface {
	PruneEvents(ctx context.Context, olderThan time.Duration) (int, error)
}

// MediaPurger membuang cache metadata kedaluwarsa.
type MediaPurger interface {
	PurgeExpired(ctx context.Context) (int, error)
}

// FileReconciler membaca seluruh berkas hasil dan menandai keberadaannya.
type FileReconciler interface {
	All(ctx context.Context) ([]*domain.File, error)
	SetMissing(ctx context.Context, id string, missing bool) error
}

// TempCollector membuang berkas sementara yatim.
type TempCollector interface {
	GCTemp(maxAge time.Duration, active map[string]bool) (int, error)
}

// HousekeepingReport merangkum hasil satu putaran housekeeping.
type HousekeepingReport struct {
	EventsPruned  int
	MediaPurged   int
	TempRemoved   int
	FilesMissing  int
	FilesRestored int
}

// HousekeepingDeps mengumpulkan dependensi Housekeeper.
type HousekeepingDeps struct {
	Events EventPruner
	Media  MediaPurger
	Files  FileReconciler
	Temp   TempCollector

	// ActiveJobs mengembalikan id job yang sedang berjalan, supaya temp
	// miliknya tidak ikut dibersihkan.
	ActiveJobs func() []string

	// Stat memeriksa keberadaan berkas; bawaannya os.Stat.
	Stat func(path string) (os.FileInfo, error)

	Log *slog.Logger
}

// Housekeeper menjalankan pemeliharaan berkala yang menjaga database dan
// disk tidak tumbuh tanpa batas.
type Housekeeper struct {
	d HousekeepingDeps
}

// NewHousekeeper membuat housekeeper.
func NewHousekeeper(d HousekeepingDeps) *Housekeeper {
	if d.Stat == nil {
		d.Stat = os.Stat
	}
	if d.ActiveJobs == nil {
		d.ActiveJobs = func() []string { return nil }
	}
	return &Housekeeper{d: d}
}

// Run menjalankan RunOnce setiap HousekeepingInterval sampai ctx selesai.
func (h *Housekeeper) Run(ctx context.Context) {
	ticker := time.NewTicker(HousekeepingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.RunOnce(ctx)
		}
	}
}

// RunOnce menjalankan seluruh tugas satu kali.
//
// Setiap tugas berdiri sendiri: kegagalan satu tugas dicatat lalu tugas
// berikutnya tetap berjalan, karena tidak ada di antaranya yang bergantung
// pada yang lain.
func (h *Housekeeper) RunOnce(ctx context.Context) HousekeepingReport {
	var r HousekeepingReport
	var err error

	if r.EventsPruned, err = h.d.Events.PruneEvents(ctx, EventRetention); err != nil {
		h.d.Log.Warn("pangkas event gagal", "error", err)
	}
	if r.MediaPurged, err = h.d.Media.PurgeExpired(ctx); err != nil {
		h.d.Log.Warn("buang cache metadata gagal", "error", err)
	}

	active := map[string]bool{}
	for _, id := range h.d.ActiveJobs() {
		active[id] = true
	}
	if r.TempRemoved, err = h.d.Temp.GCTemp(TempMaxAge, active); err != nil {
		h.d.Log.Warn("bersihkan temp gagal", "error", err)
	}

	if r.FilesMissing, r.FilesRestored, err = h.reconcileFiles(ctx); err != nil {
		h.d.Log.Warn("rekonsiliasi berkas gagal", "error", err)
	}

	if r != (HousekeepingReport{}) {
		h.d.Log.Info("housekeeping",
			"event_terpangkas", r.EventsPruned,
			"cache_terbuang", r.MediaPurged,
			"temp_terhapus", r.TempRemoved,
			"berkas_hilang", r.FilesMissing,
			"berkas_kembali", r.FilesRestored)
	}
	return r
}

// reconcileFiles menyamakan penanda missing dengan keadaan disk.
//
// Arahnya dua: berkas yang dihapus di luar aplikasi ditandai hilang, dan
// berkas yang muncul kembali dipulihkan. Yang kedua penting untuk folder
// keluaran di drive eksternal: tanpa itu, mencabut drive sekali membuat
// seluruh riwayatnya kehilangan tombol unduh selamanya.
//
// Hanya ErrNotExist yang dianggap hilang. Error lain, misalnya izin
// ditolak, tidak membuktikan apa pun tentang keberadaan berkas, jadi
// penandanya dibiarkan.
func (h *Housekeeper) reconcileFiles(ctx context.Context) (missing, restored int, err error) {
	files, err := h.d.Files.All(ctx)
	if err != nil {
		return 0, 0, err
	}

	for _, f := range files {
		if ctx.Err() != nil {
			return missing, restored, ctx.Err()
		}

		_, statErr := h.d.Stat(f.Path)
		var exists bool
		switch {
		case statErr == nil:
			exists = true
		case errors.Is(statErr, fs.ErrNotExist):
			exists = false
		default:
			continue
		}

		if exists == !f.Missing {
			continue
		}
		if err := h.d.Files.SetMissing(ctx, f.ID, !exists); err != nil {
			return missing, restored, err
		}
		if exists {
			restored++
		} else {
			missing++
		}
	}
	return missing, restored, nil
}

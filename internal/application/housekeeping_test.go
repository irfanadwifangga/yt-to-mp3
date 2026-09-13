package application_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

type fakeEvents struct {
	olderThan time.Duration
	n         int
	err       error
}

func (f *fakeEvents) PruneEvents(_ context.Context, olderThan time.Duration) (int, error) {
	f.olderThan = olderThan
	return f.n, f.err
}

type fakeMedia struct{ n int }

func (f *fakeMedia) PurgeExpired(context.Context) (int, error) { return f.n, nil }

type fakeTemp struct {
	active map[string]bool
	maxAge time.Duration
}

func (f *fakeTemp) GCTemp(maxAge time.Duration, active map[string]bool) (int, error) {
	f.maxAge = maxAge
	f.active = active
	return 0, nil
}

type fakeFiles struct {
	files   []*domain.File
	changes map[string]bool
}

func (f *fakeFiles) All(context.Context) ([]*domain.File, error) { return f.files, nil }

func (f *fakeFiles) SetMissing(_ context.Context, id string, missing bool) error {
	f.changes[id] = missing
	return nil
}

// stat meniru disk: path di ada tersedia, path di denied gagal karena
// alasan selain tidak ada.
func stat(ada, denied map[string]bool) func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		switch {
		case ada[path]:
			return nil, nil
		case denied[path]:
			return nil, fs.ErrPermission
		default:
			return nil, fs.ErrNotExist
		}
	}
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestHousekeepingRekonsiliasiDuaArah(t *testing.T) {
	files := &fakeFiles{
		files: []*domain.File{
			{ID: "dihapus", Path: "/musik/dihapus.mp3"},
			{ID: "kembali", Path: "/drive-eksternal/kembali.mp3", Missing: true},
			{ID: "tetap-ada", Path: "/musik/ada.mp3"},
			{ID: "tetap-hilang", Path: "/musik/hilang.mp3", Missing: true},
			{ID: "izin-ditolak", Path: "/terkunci/lagu.mp3"},
		},
		changes: map[string]bool{},
	}

	h := application.NewHousekeeper(application.HousekeepingDeps{
		Events: &fakeEvents{}, Media: &fakeMedia{}, Temp: &fakeTemp{}, Files: files,
		Stat: stat(
			map[string]bool{"/drive-eksternal/kembali.mp3": true, "/musik/ada.mp3": true},
			map[string]bool{"/terkunci/lagu.mp3": true},
		),
		Log: discard(),
	})

	r := h.RunOnce(context.Background())

	want := map[string]bool{"dihapus": true, "kembali": false}
	if len(files.changes) != len(want) {
		t.Errorf("perubahan = %v, mau %v", files.changes, want)
	}
	for id, missing := range want {
		if got, ok := files.changes[id]; !ok || got != missing {
			t.Errorf("%s: missing = %v (diubah %v), mau %v", id, got, ok, missing)
		}
	}
	if r.FilesMissing != 1 || r.FilesRestored != 1 {
		t.Errorf("laporan = %+v", r)
	}
}

// Temp milik job yang sedang berjalan tidak boleh ikut dibersihkan, walau
// job itu sudah berjalan lebih lama dari batas umur temp.
func TestHousekeepingMelindungiTempJobAktif(t *testing.T) {
	temp := &fakeTemp{}
	events := &fakeEvents{}

	h := application.NewHousekeeper(application.HousekeepingDeps{
		Events: events, Media: &fakeMedia{}, Temp: temp,
		Files:      &fakeFiles{changes: map[string]bool{}},
		ActiveJobs: func() []string { return []string{"job_a", "job_b"} },
		Log:        discard(),
	})
	h.RunOnce(context.Background())

	if !temp.active["job_a"] || !temp.active["job_b"] || len(temp.active) != 2 {
		t.Errorf("job aktif yang diteruskan = %v", temp.active)
	}
	if temp.maxAge != application.TempMaxAge {
		t.Errorf("maxAge = %v, mau %v", temp.maxAge, application.TempMaxAge)
	}
	if events.olderThan != application.EventRetention {
		t.Errorf("retensi event = %v, mau %v", events.olderThan, application.EventRetention)
	}
}

// Satu tugas yang gagal tidak boleh menghentikan tugas lain.
func TestHousekeepingTugasGagalTidakMenghentikanLainnya(t *testing.T) {
	media := &fakeMedia{n: 3}
	h := application.NewHousekeeper(application.HousekeepingDeps{
		Events: &fakeEvents{err: errors.New("database terkunci")},
		Media:  media, Temp: &fakeTemp{},
		Files: &fakeFiles{changes: map[string]bool{}},
		Log:   discard(),
	})

	if r := h.RunOnce(context.Background()); r.MediaPurged != 3 {
		t.Errorf("cache terbuang = %d, mau 3 walau pemangkasan event gagal", r.MediaPurged)
	}
}

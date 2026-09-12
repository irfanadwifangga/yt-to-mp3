package db_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
)

func newJob(id, sourceKey string) *domain.Job {
	return &domain.Job{
		ID:           id,
		SourceURL:    "https://www.youtube.com/watch?v=" + sourceKey,
		SourceKey:    "youtube:" + sourceKey,
		Title:        "Judul " + sourceKey,
		Status:       domain.StatusQueued,
		PresetID:     "mp3_standard",
		FilenameMode: domain.FilenameTitle,
	}
}

func wantDomainCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("mau error %s, dapat nil", code)
	}
	var derr *domain.Error
	if !errors.As(err, &derr) {
		t.Fatalf("error bukan *domain.Error: %v", err)
	}
	if derr.Code != code {
		t.Fatalf("kode = %s, mau %s (detail: %s)", derr.Code, code, derr.Detail)
	}
}

func TestJobCreateDanGet(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	want := newJob("job_1", "abc")
	if err := repo.Create(ctx, want); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	got, err := repo.Get(ctx, "job_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.SourceKey != want.SourceKey || got.Title != want.Title {
		t.Errorf("job tidak sama: %+v", got)
	}
	if got.Status != domain.StatusQueued {
		t.Errorf("status = %s, mau queued", got.Status)
	}
	// Progress NULL berarti indeterminate, bukan nol.
	if got.Progress != nil {
		t.Errorf("progress = %v, mau nil", *got.Progress)
	}
	if got.CreatedAt.IsZero() {
		t.Error("created_at tidak terisi")
	}
	if got.StartedAt != nil || got.FinishedAt != nil {
		t.Error("started_at dan finished_at seharusnya masih kosong")
	}
}

func TestJobGetTidakAda(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)

	_, err := repo.Get(context.Background(), "tidak_ada")
	wantDomainCode(t, err, domain.CodeJobNotFound)
}

// Duplikat ditegakkan index unik parsial di database, bukan pengecekan di
// kode, sehingga race antar goroutine tidak bisa menyelipkan job kembar.
func TestJobCreateDuplikatAktif(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	if err := repo.Create(ctx, newJob("job_1", "abc")); err != nil {
		t.Fatalf("Create() pertama error = %v", err)
	}

	err := repo.Create(ctx, newJob("job_2", "abc"))
	wantDomainCode(t, err, domain.CodeDuplicateActive)

	// Preset berbeda bukan duplikat.
	other := newJob("job_3", "abc")
	other.PresetID = "mp3_high"
	if err := repo.Create(ctx, other); err != nil {
		t.Errorf("preset berbeda seharusnya boleh: %v", err)
	}
}

func TestJobTransition(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	if err := repo.Create(ctx, newJob("job_1", "abc")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	ev := domain.Event{Type: domain.EventState, Payload: "resolving"}
	if err := repo.Transition(ctx, "job_1", domain.StatusQueued, domain.StatusResolving, ev); err != nil {
		t.Fatalf("Transition() error = %v", err)
	}

	got, err := repo.Get(ctx, "job_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != domain.StatusResolving {
		t.Errorf("status = %s, mau resolving", got.Status)
	}
	if got.StartedAt == nil {
		t.Error("started_at seharusnya terisi saat job meninggalkan antrean")
	}

	events, err := repo.Events(ctx, "job_1", 0)
	if err != nil {
		t.Fatalf("Events() error = %v", err)
	}
	if len(events) != 1 || events[0].Seq != 1 {
		t.Errorf("event = %+v, mau satu event dengan seq 1", events)
	}
}

func TestJobTransitionDitolak(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	if err := repo.Create(ctx, newJob("job_1", "abc")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	t.Run("melompati fase", func(t *testing.T) {
		err := repo.Transition(ctx, "job_1", domain.StatusQueued, domain.StatusCompleted,
			domain.Event{Type: domain.EventState})
		if err == nil {
			t.Fatal("transisi melompat seharusnya ditolak")
		}
	})

	// Kalau status di database sudah berubah, pemanggil yang memegang
	// keyakinan lama harus kalah, bukan menimpa.
	t.Run("from basi", func(t *testing.T) {
		err := repo.Transition(ctx, "job_1", domain.StatusDownloading, domain.StatusConverting,
			domain.Event{Type: domain.EventState})
		if err == nil {
			t.Fatal("transisi dengan from basi seharusnya ditolak")
		}
	})

	t.Run("event progress ditolak", func(t *testing.T) {
		err := repo.Transition(ctx, "job_1", domain.StatusQueued, domain.StatusResolving,
			domain.Event{Type: domain.EventType("progress")})
		if err == nil {
			t.Fatal("event progress seharusnya ditolak sebelum menyentuh database")
		}
	})
}

func TestJobTransisiTerminalMengisiFinishedAt(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	if err := repo.Create(ctx, newJob("job_1", "abc")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Transition(ctx, "job_1", domain.StatusQueued, domain.StatusCancelled,
		domain.Event{Type: domain.EventDone}); err != nil {
		t.Fatalf("Transition() error = %v", err)
	}

	got, err := repo.Get(ctx, "job_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.FinishedAt == nil {
		t.Error("finished_at seharusnya terisi pada status terminal")
	}
}

func TestClaimNextQueued(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	t.Run("antrean kosong", func(t *testing.T) {
		j, err := repo.ClaimNextQueued(ctx)
		if err != nil {
			t.Fatalf("ClaimNextQueued() error = %v", err)
		}
		if j != nil {
			t.Errorf("mau nil, dapat %s", j.ID)
		}
	})

	// FIFO: job yang lebih dulu antre harus diambil lebih dulu.
	for i, key := range []string{"aaa", "bbb", "ccc"} {
		j := newJob(fmt.Sprintf("job_%d", i), key)
		j.CreatedAt = time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC)
		if err := repo.Create(ctx, j); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	for i := range 3 {
		claimed, err := repo.ClaimNextQueued(ctx)
		if err != nil {
			t.Fatalf("ClaimNextQueued() error = %v", err)
		}
		if claimed == nil {
			t.Fatalf("iterasi %d: mau job, dapat nil", i)
		}
		want := fmt.Sprintf("job_%d", i)
		if claimed.ID != want {
			t.Errorf("claim = %s, mau %s", claimed.ID, want)
		}
		if claimed.Status != domain.StatusResolving {
			t.Errorf("status = %s, mau resolving", claimed.Status)
		}
	}

	// Semua sudah diambil; tidak boleh ada yang diambil dua kali.
	extra, err := repo.ClaimNextQueued(ctx)
	if err != nil {
		t.Fatalf("ClaimNextQueued() error = %v", err)
	}
	if extra != nil {
		t.Errorf("job %s diambil dua kali", extra.ID)
	}
}

// Job aktif saat startup berarti proses pemiliknya sudah mati, jadi tidak
// mungkin dilanjutkan. Job queued dibiarkan supaya dapat dijadwalkan ulang.
func TestSweepNonTerminal(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	if err := repo.Create(ctx, newJob("job_queued", "aaa")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Create(ctx, newJob("job_aktif", "bbb")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Transition(ctx, "job_aktif", domain.StatusQueued, domain.StatusResolving,
		domain.Event{Type: domain.EventState}); err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if err := repo.Transition(ctx, "job_aktif", domain.StatusResolving, domain.StatusDownloading,
		domain.Event{Type: domain.EventState}); err != nil {
		t.Fatalf("Transition() error = %v", err)
	}

	n, err := repo.SweepNonTerminal(ctx)
	if err != nil {
		t.Fatalf("SweepNonTerminal() error = %v", err)
	}
	if n != 1 {
		t.Errorf("tersapu %d job, mau 1", n)
	}

	aktif, err := repo.Get(ctx, "job_aktif")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if aktif.Status != domain.StatusFailed {
		t.Errorf("status = %s, mau failed", aktif.Status)
	}
	if aktif.ErrorCode != domain.CodeInterrupted {
		t.Errorf("error_code = %s, mau %s", aktif.ErrorCode, domain.CodeInterrupted)
	}

	queued, err := repo.Get(ctx, "job_queued")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if queued.Status != domain.StatusQueued {
		t.Errorf("job antre ikut tersapu, status = %s", queued.Status)
	}
}

func TestJobListPagination(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	for i := range 5 {
		j := newJob(fmt.Sprintf("job_%d", i), fmt.Sprintf("key%d", i))
		j.CreatedAt = time.Date(2026, 1, 1, 0, i, 0, 0, time.UTC)
		if err := repo.Create(ctx, j); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	seen := map[string]bool{}
	cursor := ""
	for page := range 5 {
		jobs, next, err := repo.List(ctx, application.JobListQuery{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		for _, j := range jobs {
			if seen[j.ID] {
				t.Errorf("job %s muncul dua kali", j.ID)
			}
			seen[j.ID] = true
		}
		if next == "" {
			break
		}
		cursor = next
		if page == 4 {
			t.Fatal("pagination tidak pernah selesai")
		}
	}

	if len(seen) != 5 {
		t.Errorf("terbaca %d job, mau 5", len(seen))
	}
}

func TestJobListFilterStatus(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	if err := repo.Create(ctx, newJob("job_1", "aaa")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Create(ctx, newJob("job_2", "bbb")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Transition(ctx, "job_2", domain.StatusQueued, domain.StatusCancelled,
		domain.Event{Type: domain.EventDone}); err != nil {
		t.Fatalf("Transition() error = %v", err)
	}

	jobs, _, err := repo.List(ctx, application.JobListQuery{Status: domain.StatusCancelled})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job_2" {
		t.Errorf("filter status salah: %+v", jobs)
	}
}

func TestJobDelete(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	if err := repo.Create(ctx, newJob("job_1", "abc")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repo.Delete(ctx, "job_1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	wantDomainCode(t, repo.Delete(ctx, "job_1"), domain.CodeJobNotFound)
}

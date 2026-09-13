package worker_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
	"github.com/irfanadwifangga/yt-to-mp3/internal/worker"
)

// runnerFunc mengubah fungsi biasa jadi JobRunner.
type runnerFunc func(ctx context.Context, job *domain.Job) error

func (f runnerFunc) Run(ctx context.Context, job *domain.Job) error { return f(ctx, job) }

// recorder mencatat event yang disiarkan scheduler.
type recorder struct {
	mu     sync.Mutex
	events []application.StreamEvent
}

func (r *recorder) Publish(ev application.StreamEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) statuses() []domain.JobStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.JobStatus, 0, len(r.events))
	for _, ev := range r.events {
		out = append(out, ev.Status)
	}
	return out
}

type fixture struct {
	repo   *db.JobRepository
	sched  *worker.Scheduler
	events *recorder
}

// newFixture menyiapkan scheduler di atas database sungguhan, sehingga yang
// diuji adalah perilaku antrean beserta transisinya, bukan tiruan.
func newFixture(t *testing.T, concurrency int, run runnerFunc) *fixture {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("buat dir: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	database, err := db.Open(context.Background(), filepath.Join(dir, "app.db"), log)
	if err != nil {
		t.Fatalf("buka database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := database.Migrate(context.Background(), "test"); err != nil {
		t.Fatalf("migrasi: %v", err)
	}

	repo := db.NewJobRepository(database)
	rec := &recorder{}
	sched := worker.New(repo, run, rec, concurrency, log)
	// Bawaan test: tanpa auto-retry, supaya test lain yang memakai error
	// transient tidak menunggu jeda sungguhan. Test auto-retry memasang
	// kebijakannya sendiri.
	sched.SetRetryPolicy(func(*domain.Error, int) (time.Duration, bool) { return 0, false }, 0)
	return &fixture{
		repo:   repo,
		sched:  sched,
		events: rec,
	}
}

// quickRetry mengulang kelas transient dan throttled tanpa jeda, dengan
// batas yang sama seperti kebijakan sungguhan.
func quickRetry(err *domain.Error, attempt int) (time.Duration, bool) {
	if attempt >= application.MaxAutoRetries {
		return 0, false
	}
	switch err.Class {
	case domain.ClassTransient, domain.ClassThrottled:
		return 0, true
	}
	return 0, false
}

// complete menjalankan jalur sukses lengkap dari resolving.
func complete(ctx context.Context, repo *db.JobRepository, id string) error {
	steps := []domain.JobStatus{
		domain.StatusResolving, domain.StatusDownloading, domain.StatusConverting,
		domain.StatusVerifying, domain.StatusCompleted,
	}
	for i := 1; i < len(steps); i++ {
		if err := repo.Transition(ctx, id, steps[i-1], steps[i], domain.Event{}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fixture) enqueue(t *testing.T, id, key string) {
	t.Helper()
	err := f.repo.Create(context.Background(), &domain.Job{
		ID:           id,
		SourceURL:    "https://www.youtube.com/watch?v=" + key,
		SourceKey:    "youtube:" + key,
		Status:       domain.StatusQueued,
		PresetID:     "mp3_standard",
		FilenameMode: domain.FilenameTitle,
	})
	if err != nil {
		t.Fatalf("antrekan job: %v", err)
	}
}

// run menjalankan scheduler sampai fn selesai, lalu menghentikannya.
func (f *fixture) run(t *testing.T, fn func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.sched.Run(ctx)
		close(done)
	}()

	fn()
	cancel()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("scheduler tidak berhenti tepat waktu")
	}
}

// waitStatus menunggu job mencapai status tertentu.
func (f *fixture) waitStatus(t *testing.T, id string, want domain.JobStatus) *domain.Job {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var last domain.JobStatus
	for time.Now().Before(deadline) {
		job, err := f.repo.Get(context.Background(), id)
		if err == nil {
			last = job.Status
			if job.Status == want {
				return job
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s berstatus %s, tidak pernah mencapai %s", id, last, want)
	return nil
}

// Runner yang menutup jobnya sendiri: scheduler tidak boleh ikut menulis.
func TestRunnerMenutupJobnyaSendiri(t *testing.T) {
	var f *fixture
	f = newFixture(t, 2, func(ctx context.Context, job *domain.Job) error {
		return f.repo.Transition(ctx, job.ID, domain.StatusResolving, domain.StatusFailed,
			domain.Event{Type: domain.EventError, Payload: "selesai dari runner"})
	})

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		job := f.waitStatus(t, "job_1", domain.StatusFailed)
		// error_code kosong membuktikan scheduler tidak menimpa hasil runner.
		if job.ErrorCode != "" {
			t.Errorf("error_code = %q, scheduler seharusnya tidak menimpa", job.ErrorCode)
		}
	})
}

func TestRunnerGagalDitandaiFailed(t *testing.T) {
	f := newFixture(t, 2, func(ctx context.Context, job *domain.Job) error {
		return domain.NewError(domain.CodeVideoPrivate, domain.ClassPermanent, "privat")
	})

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		job := f.waitStatus(t, "job_1", domain.StatusFailed)
		if job.ErrorCode != domain.CodeVideoPrivate {
			t.Errorf("error_code = %s, mau %s", job.ErrorCode, domain.CodeVideoPrivate)
		}
	})
}

// Runner yang mengembalikan nil tanpa menutup job melanggar kontrak; itu
// bug, dan pesannya harus menyebut sebabnya, bukan INTERNAL kosong.
func TestRunnerLupaMenutupJob(t *testing.T) {
	f := newFixture(t, 2, func(ctx context.Context, job *domain.Job) error {
		return nil
	})

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		job := f.waitStatus(t, "job_1", domain.StatusFailed)
		if job.ErrorCode != domain.CodeInternal {
			t.Errorf("error_code = %s, mau INTERNAL", job.ErrorCode)
		}
		if job.ErrorMessage == "" {
			t.Error("pesan error kosong; seharusnya menjelaskan pelanggaran kontrak")
		}
	})
}

// Cancel pada job yang sedang berjalan harus menghentikan runner lewat
// context, lalu ditutup oleh goroutine pemilik job.
func TestCancelJobBerjalan(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once

	f := newFixture(t, 2, func(ctx context.Context, job *domain.Job) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	})

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("runner tidak pernah mulai")
		}

		if err := f.sched.Cancel(context.Background(), "job_1"); err != nil {
			t.Fatalf("Cancel() error = %v", err)
		}
		f.waitStatus(t, "job_1", domain.StatusCancelled)
	})
}

// Job yang masih antre belum memegang proses OS, jadi boleh ditutup
// langsung tanpa melewati cancelling.
func TestCancelJobAntre(t *testing.T) {
	f := newFixture(t, 1, func(ctx context.Context, job *domain.Job) error {
		<-ctx.Done()
		return ctx.Err()
	})

	f.enqueue(t, "job_1", "aaa")
	if err := f.sched.Cancel(context.Background(), "job_1"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	job, err := f.repo.Get(context.Background(), "job_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if job.Status != domain.StatusCancelled {
		t.Errorf("status = %s, mau cancelled", job.Status)
	}
}

// Batas konkurensi adalah kontrol beban yang nyata: lebih dari dua unduhan
// paralel dari satu IP memicu throttling di sisi sumber.
func TestKonkurensiDibatasi(t *testing.T) {
	var concurrent, peak atomic.Int32
	release := make(chan struct{})

	f := newFixture(t, 1, func(ctx context.Context, job *domain.Job) error {
		n := concurrent.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		<-release
		concurrent.Add(-1)
		return domain.NewError(domain.CodeInternal, domain.ClassLocal, "selesai")
	})

	f.enqueue(t, "job_1", "aaa")
	f.enqueue(t, "job_2", "bbb")
	f.enqueue(t, "job_3", "ccc")

	f.run(t, func() {
		deadline := time.Now().Add(5 * time.Second)
		for concurrent.Load() == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond) // beri kesempatan slot kedua terpakai
		close(release)

		f.waitStatus(t, "job_1", domain.StatusFailed)
		f.waitStatus(t, "job_2", domain.StatusFailed)
		f.waitStatus(t, "job_3", domain.StatusFailed)
	})

	if got := peak.Load(); got > 1 {
		t.Errorf("puncak konkurensi = %d, mau maksimum 1", got)
	}
}

// Shutdown harus membatalkan job aktif, bukan meninggalkannya menggantung.
func TestShutdownMembatalkanJobAktif(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once

	f := newFixture(t, 2, func(ctx context.Context, job *domain.Job) error {
		once.Do(func() { close(started) })
		<-ctx.Done()
		return ctx.Err()
	})

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		select {
		case <-started:
		case <-time.After(10 * time.Second):
			t.Fatal("runner tidak pernah mulai")
		}
	})

	job, err := f.repo.Get(context.Background(), "job_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if job.Status != domain.StatusCancelled {
		t.Errorf("status = %s, mau cancelled setelah shutdown", job.Status)
	}
	if f.sched.Running() != 0 {
		t.Errorf("masih ada %d job berjalan setelah shutdown", f.sched.Running())
	}
}

func TestEventDisiarkan(t *testing.T) {
	f := newFixture(t, 2, func(ctx context.Context, job *domain.Job) error {
		return domain.NewError(domain.CodeDownloadFailed, domain.ClassTransient, "gagal")
	})

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		f.waitStatus(t, "job_1", domain.StatusFailed)
	})

	var sawResolving, sawFailed bool
	for _, s := range f.events.statuses() {
		switch s {
		case domain.StatusResolving:
			sawResolving = true
		case domain.StatusFailed:
			sawFailed = true
		}
	}
	if !sawResolving || !sawFailed {
		t.Errorf("event tidak lengkap: %v", f.events.statuses())
	}
}

// Kegagalan sementara diulang otomatis di baris job yang sama, lalu job
// selesai normal begitu percobaan berikutnya berhasil.
func TestAutoRetrySampaiBerhasil(t *testing.T) {
	var calls atomic.Int32
	var f *fixture
	f = newFixture(t, 1, func(ctx context.Context, job *domain.Job) error {
		if calls.Add(1) <= 2 {
			return domain.NewError(domain.CodeDownloadFailed, domain.ClassTransient, "connection reset")
		}
		return complete(ctx, f.repo, job.ID)
	})
	f.sched.SetRetryPolicy(quickRetry, 0)

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		job := f.waitStatus(t, "job_1", domain.StatusCompleted)
		if job.AttemptCount != 2 {
			t.Errorf("attempt_count = %d, mau 2", job.AttemptCount)
		}
		if job.ErrorCode != "" || job.RetryAt != nil {
			t.Errorf("sisa percobaan gagal tertinggal: code=%q retry_at=%v", job.ErrorCode, job.RetryAt)
		}
	})
	if got := calls.Load(); got != 3 {
		t.Errorf("runner dipanggil %d kali, mau 3", got)
	}
}

// Setelah jatah habis, job ditutup sebagai failed dengan kode terakhirnya.
func TestAutoRetryBerhentiSetelahBatas(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, 1, func(ctx context.Context, job *domain.Job) error {
		calls.Add(1)
		return domain.NewError(domain.CodeDownloadFailed, domain.ClassTransient, "timed out")
	})
	f.sched.SetRetryPolicy(quickRetry, 0)

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		job := f.waitStatus(t, "job_1", domain.StatusFailed)
		if job.ErrorCode != domain.CodeDownloadFailed {
			t.Errorf("error_code = %s, mau %s", job.ErrorCode, domain.CodeDownloadFailed)
		}
		if job.AttemptCount != application.MaxAutoRetries {
			t.Errorf("attempt_count = %d, mau %d", job.AttemptCount, application.MaxAutoRetries)
		}
	})
	if got, want := calls.Load(), int32(1+application.MaxAutoRetries); got != want {
		t.Errorf("runner dipanggil %d kali, mau %d", got, want)
	}
}

// Kegagalan permanen tidak pernah diulang, walau kebijakan retry aktif.
func TestAutoRetryTidakMengulangKegagalanPermanen(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, 1, func(ctx context.Context, job *domain.Job) error {
		calls.Add(1)
		return domain.NewError(domain.CodeVideoPrivate, domain.ClassPermanent, "privat")
	})
	f.sched.SetRetryPolicy(quickRetry, 0)

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		f.waitStatus(t, "job_1", domain.StatusFailed)
	})
	if got := calls.Load(); got != 1 {
		t.Errorf("runner dipanggil %d kali, mau 1", got)
	}
}

// Setelah 429, job paralel diturunkan ke satu selama masa pendinginan.
func TestThrottleMenurunkanKonkurensi(t *testing.T) {
	var (
		throttledOnce atomic.Bool
		afterThrottle atomic.Bool
		concurrent    atomic.Int32
		peak          atomic.Int32
	)
	release := make(chan struct{})

	f := newFixture(t, 2, func(ctx context.Context, job *domain.Job) error {
		if job.ID == "job_1" && throttledOnce.CompareAndSwap(false, true) {
			return domain.NewError(domain.CodeRateLimited, domain.ClassThrottled, "http error 429")
		}

		n := concurrent.Add(1)
		defer concurrent.Add(-1)
		if afterThrottle.Load() {
			for {
				old := peak.Load()
				if n <= old || peak.CompareAndSwap(old, n) {
					break
				}
			}
		}
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return domain.NewError(domain.CodeInternal, domain.ClassLocal, "selesai")
	})
	f.sched.SetRetryPolicy(quickRetry, time.Minute)

	f.enqueue(t, "job_1", "aaa")
	f.run(t, func() {
		deadline := time.Now().Add(5 * time.Second)
		for !throttledOnce.Load() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		// Beri waktu job_1 kembali ke antrean sebelum job lain ditambahkan.
		time.Sleep(100 * time.Millisecond)
		afterThrottle.Store(true)

		f.enqueue(t, "job_2", "bbb")
		f.enqueue(t, "job_3", "ccc")
		f.sched.Notify() <- struct{}{}

		time.Sleep(400 * time.Millisecond) // beri kesempatan slot kedua terpakai bila tidak dibatasi
		close(release)

		for _, id := range []string{"job_1", "job_2", "job_3"} {
			f.waitStatus(t, id, domain.StatusFailed)
		}
	})

	if got := peak.Load(); got != 1 {
		t.Errorf("puncak konkurensi selama pendinginan = %d, mau 1", got)
	}
}

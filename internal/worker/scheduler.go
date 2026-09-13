// Package worker memuat scheduler dan bounded worker pool yang menjalankan
// job.
//
// Mesin antrean di sini tidak tahu apa pun tentang yt-dlp maupun FFmpeg:
// isi pekerjaan datang lewat port application.JobRunner. Pemisahan itu yang
// membuat seluruh perilaku antrean dapat diuji tanpa menyentuh jaringan.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// pollInterval adalah jaring pengaman bila sinyal notify terlewat.
const pollInterval = 5 * time.Second

// Scheduler mengambil job antre dan menjalankannya dengan konkurensi
// terbatas.
type Scheduler struct {
	repo   application.JobRepository
	runner application.JobRunner
	events application.EventPublisher
	log    *slog.Logger

	notify chan struct{}
	slots  chan struct{}

	mu             sync.Mutex
	running        map[string]context.CancelFunc
	throttledUntil time.Time // dijaga mu

	retryPlan        RetryPolicy
	throttleCooldown time.Duration

	wg sync.WaitGroup
}

// RetryPolicy menentukan jeda auto-retry untuk sebuah kegagalan; ok false
// berarti job ditutup sebagai failed.
type RetryPolicy func(err *domain.Error, attempt int) (delay time.Duration, ok bool)

// New membuat scheduler dengan batas konkurensi tertentu.
func New(
	repo application.JobRepository,
	runner application.JobRunner,
	events application.EventPublisher,
	concurrency int,
	log *slog.Logger,
) *Scheduler {
	if concurrency < 1 {
		concurrency = 1
	}
	return &Scheduler{
		repo:    repo,
		runner:  runner,
		events:  events,
		log:     log,
		notify:  make(chan struct{}, 1),
		slots:   make(chan struct{}, concurrency),
		running: make(map[string]context.CancelFunc),
		retryPlan: func(err *domain.Error, attempt int) (time.Duration, bool) {
			return application.RetryPlan(err, attempt, rand.Float64)
		},
		throttleCooldown: application.ThrottleCooldown,
	}
}

// SetRetryPolicy mengganti kebijakan auto-retry dan lama penurunan
// konkurensi setelah throttling. Dipakai test supaya tidak menunggu jeda
// sungguhan.
func (s *Scheduler) SetRetryPolicy(plan RetryPolicy, throttleCooldown time.Duration) {
	s.retryPlan = plan
	s.throttleCooldown = throttleCooldown
}

// Notify mengembalikan kanal pembangun yang dipakai JobService.
func (s *Scheduler) Notify() chan<- struct{} { return s.notify }

// Run menjalankan loop penjadwalan sampai ctx dibatalkan.
//
// Setelah ctx selesai, seluruh job yang sedang berjalan ikut dibatalkan dan
// ditunggu sampai benar-benar berhenti, supaya tidak ada proses anak yang
// tertinggal.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	s.drain(ctx) // job yang tersisa dari sesi sebelumnya

	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return
		case <-s.notify:
			s.drain(ctx)
		case <-ticker.C:
			s.drain(ctx)
		}
	}
}

// drain mengambil job selama masih ada slot kosong dan antrean berisi.
func (s *Scheduler) drain(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case s.slots <- struct{}{}:
		default:
			return // seluruh slot terpakai
		}

		// Setelah sumber membalas 429, satu job saja yang boleh berjalan
		// sampai masa pendinginan lewat. Menjalankan beberapa unduhan
		// sekaligus justru memperpanjang pembatasan.
		if s.throttled() && s.Running() > 0 {
			<-s.slots
			return
		}

		job, err := s.repo.ClaimNextQueued(ctx)
		if err != nil {
			<-s.slots
			s.log.Error("claim job gagal", "error", err)
			return
		}
		if job == nil {
			<-s.slots
			return // antrean kosong
		}

		s.start(ctx, job)
	}
}

// start menjalankan satu job pada goroutine tersendiri.
func (s *Scheduler) start(parent context.Context, job *domain.Job) {
	jobCtx, cancel := context.WithCancel(parent)

	s.mu.Lock()
	s.running[job.ID] = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, job.ID)
			s.mu.Unlock()
			cancel()
			<-s.slots

			// Slot baru saja kosong. Tanpa sinyal ini, job berikutnya
			// menunggu tick berikutnya walau pekerjaannya sudah siap,
			// sehingga antrean menganggur sampai lima detik per job.
			select {
			case s.notify <- struct{}{}:
			default:
			}

			s.wg.Done()
		}()

		s.publishState(job.ID, domain.StatusResolving)
		s.log.Info("job dimulai", "job", job.ID, "source", job.SourceKey)

		err := s.runner.Run(jobCtx, job)
		s.finish(jobCtx, job, err)
	}()
}

// finish menulis hasil akhir job.
//
// Runner yang sudah menutup jobnya sendiri dibiarkan apa adanya; scheduler
// hanya menutup job yang masih menggantung, sehingga tidak ada dua penulis
// untuk status terminal yang sama.
func (s *Scheduler) finish(ctx context.Context, job *domain.Job, runErr error) {
	// Context terpisah: pembersihan harus tetap tertulis walau job baru saja
	// dibatalkan lewat pembatalan context.
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	current, err := s.repo.Get(closeCtx, job.ID)
	if err != nil {
		s.log.Error("baca status akhir gagal", "job", job.ID, "error", err)
		return
	}
	if current.Status.IsTerminal() {
		s.publishDone(current)
		return
	}

	if errors.Is(runErr, context.Canceled) || ctx.Err() != nil {
		s.closeCancelled(closeCtx, current)
		return
	}

	code := domain.CodeInternal
	detail := ""
	var derr *domain.Error
	switch {
	case errors.As(runErr, &derr):
		code = derr.Code
		detail = derr.Detail
	case runErr != nil:
		detail = runErr.Error()
	default:
		// Kontrak JobRunner: sukses berarti job sudah ditutup sendiri ke
		// status terminal. Sampai di sini dengan runErr nil berarti runner
		// melanggar kontrak, dan itu bug, bukan kegagalan biasa.
		detail = "runner selesai tanpa menutup job ke status terminal"
		s.log.Error(detail, "job", job.ID, "status", current.Status)
	}

	if derr != nil && s.retryLater(closeCtx, current, derr) {
		return
	}

	if err := s.repo.Fail(closeCtx, job.ID, current.Status, code, detail); err != nil {
		s.log.Error("tandai job gagal", "job", job.ID, "error", err)
		return
	}
	s.log.Warn("job gagal", "job", job.ID, "code", code, "detail", detail)
	s.events.Publish(application.StreamEvent{
		JobID: job.ID, Type: application.StreamError,
		Status: domain.StatusFailed, Code: code,
	})
}

// retryLater mengembalikan job ke antrean bila kegagalannya sementara.
//
// Jeda disimpan sebagai retry_at di database, sehingga tetap dihormati walau
// aplikasi dibuka ulang di tengah jeda. Timer di sini hanya membangunkan
// scheduler tepat waktu; tanpa timer pun job akan terambil pada poll
// berikutnya.
func (s *Scheduler) retryLater(ctx context.Context, job *domain.Job, derr *domain.Error) bool {
	delay, ok := s.retryPlan(derr, job.AttemptCount)
	if !ok {
		return false
	}

	if err := s.repo.Requeue(ctx, job.ID, job.Status, derr.Code, derr.Detail, time.Now().Add(delay)); err != nil {
		s.log.Error("antrekan ulang gagal, job ditandai gagal", "job", job.ID, "error", err)
		return false
	}

	if derr.Class == domain.ClassThrottled {
		until := time.Now().Add(s.throttleCooldown)
		s.mu.Lock()
		if until.After(s.throttledUntil) {
			s.throttledUntil = until
		}
		s.mu.Unlock()
		s.log.Warn("sumber membatasi permintaan, job paralel diturunkan ke satu",
			"selama", s.throttleCooldown)
	}

	s.log.Warn("job gagal sementara, dicoba lagi",
		"job", job.ID, "code", derr.Code,
		"percobaan", job.AttemptCount+1, "maks", application.MaxAutoRetries, "jeda", delay)
	s.events.Publish(application.StreamEvent{
		JobID: job.ID, Type: application.StreamState,
		Status: domain.StatusQueued, Code: derr.Code,
	})

	time.AfterFunc(delay, func() {
		select {
		case s.notify <- struct{}{}:
		default:
		}
	})
	return true
}

// throttled melaporkan apakah masa pendinginan setelah 429 masih berjalan.
func (s *Scheduler) throttled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Before(s.throttledUntil)
}

// closeCancelled menuntaskan job yang dibatalkan, lewat cancelling bila
// statusnya memang mengharuskan.
func (s *Scheduler) closeCancelled(ctx context.Context, job *domain.Job) {
	from := job.Status
	if from != domain.StatusCancelling && domain.CanTransition(from, domain.StatusCancelling) {
		if err := s.repo.Transition(ctx, job.ID, from, domain.StatusCancelling,
			domain.Event{Type: domain.EventState, Payload: cancellingPayload}); err != nil {
			s.log.Error("transisi ke cancelling gagal", "job", job.ID, "error", err)
			return
		}
		from = domain.StatusCancelling
	}

	if err := s.repo.Transition(ctx, job.ID, from, domain.StatusCancelled,
		domain.Event{Type: domain.EventDone, Payload: cancelledPayload}); err != nil {
		s.log.Error("transisi ke cancelled gagal", "job", job.ID, "error", err)
		return
	}
	s.log.Info("job dibatalkan", "job", job.ID)
	s.events.Publish(application.StreamEvent{
		JobID: job.ID, Type: application.StreamDone, Status: domain.StatusCancelled,
	})
}

// Cancel membatalkan job, baik yang sedang berjalan maupun yang masih antre.
//
// Handler HTTP hanya memicu; yang menulis status terminal tetap goroutine
// pemilik job, setelah prosesnya benar-benar berhenti.
func (s *Scheduler) Cancel(ctx context.Context, jobID string) error {
	job, err := s.repo.Get(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status.IsTerminal() {
		return domain.NewError(domain.CodeInternal, domain.ClassLocal,
			"job sudah selesai")
	}

	s.mu.Lock()
	cancel, running := s.running[jobID]
	s.mu.Unlock()

	if !running {
		// Belum ada proses OS yang dipegang, jadi boleh langsung ditutup.
		return s.repo.Transition(ctx, jobID, job.Status, domain.StatusCancelled,
			domain.Event{Type: domain.EventDone, Payload: cancelledPayload})
	}

	if domain.CanTransition(job.Status, domain.StatusCancelling) {
		if err := s.repo.Transition(ctx, jobID, job.Status, domain.StatusCancelling,
			domain.Event{Type: domain.EventState, Payload: cancellingPayload}); err != nil {
			return err
		}
	}
	cancel()
	return nil
}

// Running melaporkan jumlah job yang sedang berjalan.
func (s *Scheduler) Running() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running)
}

// RunningIDs mengembalikan id job yang sedang berjalan.
func (s *Scheduler) RunningIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.running))
	for id := range s.running {
		ids = append(ids, id)
	}
	return ids
}

// shutdown membatalkan seluruh job aktif dan menunggu mereka berhenti.
func (s *Scheduler) shutdown() {
	s.mu.Lock()
	for _, cancel := range s.running {
		cancel()
	}
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.log.Info("seluruh job berhenti dengan bersih")
	case <-time.After(10 * time.Second):
		s.log.Warn("sebagian job belum berhenti setelah batas waktu")
	}
}

func (s *Scheduler) publishState(jobID string, status domain.JobStatus) {
	s.events.Publish(application.StreamEvent{
		JobID: jobID, Type: application.StreamState, Status: status,
	})
}

func (s *Scheduler) publishDone(job *domain.Job) {
	s.events.Publish(application.StreamEvent{
		JobID: job.ID, Type: application.StreamDone, Status: job.Status, Code: job.ErrorCode,
	})
}

// Payload event terpersist memakai bentuk yang sama dengan event live,
// sehingga pelanggan yang menyambung ulang tidak menerima struktur berbeda.
var (
	cancellingPayload = application.StreamEvent{
		Type: application.StreamState, Status: domain.StatusCancelling,
	}.PayloadJSON()

	cancelledPayload = application.StreamEvent{
		Type: application.StreamDone, Status: domain.StatusCancelled,
	}.PayloadJSON()
)

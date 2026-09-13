package db_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// Event job yang lama selesai dipangkas; job itu sendiri, event job yang
// baru selesai, dan event job yang masih aktif dibiarkan.
func TestPruneEvents(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	countEvents := func(jobID string) int {
		t.Helper()
		var n int
		if err := d.Read().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM job_events WHERE job_id = ?`, jobID).Scan(&n); err != nil {
			t.Fatalf("hitung event: %v", err)
		}
		return n
	}

	for id, key := range map[string]string{"job_lama": "aaa", "job_baru": "bbb", "job_aktif": "ccc"} {
		if err := repo.Create(ctx, newJob(id, key)); err != nil {
			t.Fatalf("Create(%s) error = %v", id, err)
		}
	}
	for _, id := range []string{"job_lama", "job_baru"} {
		if err := repo.Transition(ctx, id, domain.StatusQueued, domain.StatusCancelled,
			domain.Event{Type: domain.EventDone}); err != nil {
			t.Fatalf("Transition(%s) error = %v", id, err)
		}
	}
	if err := repo.Transition(ctx, "job_aktif", domain.StatusQueued, domain.StatusResolving,
		domain.Event{Type: domain.EventState}); err != nil {
		t.Fatalf("Transition(job_aktif) error = %v", err)
	}

	old := time.Now().UTC().AddDate(0, 0, -45).Format(time.RFC3339Nano)
	if _, err := d.Write().ExecContext(ctx,
		`UPDATE jobs SET finished_at = ? WHERE id = 'job_lama'`, old); err != nil {
		t.Fatalf("mundurkan finished_at: %v", err)
	}

	before := map[string]int{
		"job_lama": countEvents("job_lama"), "job_baru": countEvents("job_baru"),
		"job_aktif": countEvents("job_aktif"),
	}

	n, err := repo.PruneEvents(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("PruneEvents() error = %v", err)
	}
	if n != before["job_lama"] || n == 0 {
		t.Errorf("terpangkas %d event, mau %d", n, before["job_lama"])
	}

	if got := countEvents("job_lama"); got != 0 {
		t.Errorf("event job lama tersisa %d", got)
	}
	for _, id := range []string{"job_baru", "job_aktif"} {
		if got := countEvents(id); got != before[id] {
			t.Errorf("event %s = %d, mau tetap %d", id, got, before[id])
		}
	}
	if _, err := repo.Get(ctx, "job_lama"); err != nil {
		t.Errorf("job lama ikut terhapus: %v", err)
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

// Job yang diantrekan ulang menunggu retry_at, lalu diambil kembali dengan
// sisa kegagalan sebelumnya dibersihkan.
func TestRequeueMenghormatiRetryAt(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	if err := repo.Create(ctx, newJob("job_1", "aaa")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	claimed, err := repo.ClaimNextQueued(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("ClaimNextQueued() = %v, %v", claimed, err)
	}

	if err := repo.Requeue(ctx, "job_1", domain.StatusResolving,
		domain.CodeDownloadFailed, "connection reset", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Requeue() error = %v", err)
	}

	waiting, err := repo.Get(ctx, "job_1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if waiting.Status != domain.StatusQueued || waiting.AttemptCount != 1 ||
		waiting.ErrorCode != domain.CodeDownloadFailed || waiting.RetryAt == nil {
		t.Errorf("job menunggu = %+v", waiting)
	}

	if next, err := repo.ClaimNextQueued(ctx); err != nil || next != nil {
		t.Fatalf("job diambil sebelum retry_at: %v, %v", next, err)
	}

	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := d.Write().ExecContext(ctx, `UPDATE jobs SET retry_at = ? WHERE id = 'job_1'`, past); err != nil {
		t.Fatal(err)
	}

	again, err := repo.ClaimNextQueued(ctx)
	if err != nil || again == nil {
		t.Fatalf("job tidak diambil setelah retry_at lewat: %v, %v", again, err)
	}
	if again.ErrorCode != "" || again.RetryAt != nil || again.AttemptCount != 1 {
		t.Errorf("job diambil ulang = %+v", again)
	}

	// Job yang sudah berganti status tidak boleh diantrekan ulang oleh
	// penulis yang keyakinannya basi.
	if err := repo.Requeue(ctx, "job_1", domain.StatusDownloading,
		domain.CodeDownloadFailed, "x", time.Now()); err == nil {
		t.Error("Requeue dengan status asal yang basi seharusnya ditolak")
	}
}

func TestSetTitleHanyaMengisiYangKosong(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	kosong := newJob("job_kosong", "aaa")
	kosong.Title = ""
	for _, j := range []*domain.Job{kosong, newJob("job_berjudul", "bbb")} {
		if err := repo.Create(ctx, j); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	for _, id := range []string{"job_kosong", "job_berjudul"} {
		if err := repo.SetTitle(ctx, id, "Judul dari metadata"); err != nil {
			t.Fatalf("SetTitle(%s) error = %v", id, err)
		}
	}

	if j, _ := repo.Get(ctx, "job_kosong"); j.Title != "Judul dari metadata" {
		t.Errorf("judul job kosong = %q", j.Title)
	}
	if j, _ := repo.Get(ctx, "job_berjudul"); j.Title != "Judul bbb" {
		t.Errorf("judul yang sudah ada tertimpa: %q", j.Title)
	}
}

func TestJobListScope(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	for id, key := range map[string]string{"job_antre": "aaa", "job_batal": "bbb", "job_jalan": "ccc"} {
		if err := repo.Create(ctx, newJob(id, key)); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	if err := repo.Transition(ctx, "job_batal", domain.StatusQueued, domain.StatusCancelled, domain.Event{}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Transition(ctx, "job_jalan", domain.StatusQueued, domain.StatusResolving, domain.Event{}); err != nil {
		t.Fatal(err)
	}

	ids := func(scope application.JobScope) map[string]bool {
		t.Helper()
		jobs, _, err := repo.List(ctx, application.JobListQuery{Scope: scope, Limit: 10})
		if err != nil {
			t.Fatalf("List(%q) error = %v", scope, err)
		}
		out := map[string]bool{}
		for _, j := range jobs {
			out[j.ID] = true
		}
		return out
	}

	if got := ids(application.ScopeActive); len(got) != 2 || !got["job_antre"] || !got["job_jalan"] {
		t.Errorf("active = %v", got)
	}
	if got := ids(application.ScopeFinished); len(got) != 1 || !got["job_batal"] {
		t.Errorf("finished = %v", got)
	}

	// Riwayat selesai harus dilayani indeks terurut tanpa sort di memori;
	// regresinya baru terasa pada ribuan job.
	plan, err := d.Read().QueryContext(ctx, `EXPLAIN QUERY PLAN
		SELECT id FROM jobs
		WHERE NOT status IN ('queued','resolving','downloading','converting','verifying','cancelling')
		ORDER BY created_at DESC, id DESC LIMIT 26`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = plan.Close() }()
	for plan.Next() {
		var id, parent, notused int
		var detail string
		if err := plan.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "TEMP B-TREE") {
			t.Errorf("riwayat selesai diurutkan di memori: %s", detail)
		}
	}
}

func TestJobListPerSumber(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	for id, key := range map[string]string{"job_a": "aaa", "job_b": "bbb"} {
		if err := repo.Create(ctx, newJob(id, key)); err != nil {
			t.Fatalf("Create(%s) error = %v", id, err)
		}
	}

	jobs, _, err := repo.List(ctx, application.JobListQuery{SourceKey: "youtube:aaa", Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job_a" {
		t.Errorf("hasil = %d job, mau hanya job_a", len(jobs))
	}
}

// Suntingan judul dan artis tersimpan per job; kosong kembali sebagai
// string kosong, bukan NULL yang bocor ke domain.
func TestJobTagSuntingan(t *testing.T) {
	d := migrated(t)
	repo := db.NewJobRepository(d)
	ctx := context.Background()

	edited := newJob("job_tag", "aaa")
	edited.TagTitle = "Bohemian Rhapsody"
	edited.TagArtist = "Queen"
	plain := newJob("job_polos", "bbb")
	for _, j := range []*domain.Job{edited, plain} {
		if err := repo.Create(ctx, j); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	got, err := repo.Get(ctx, "job_tag")
	if err != nil {
		t.Fatal(err)
	}
	if got.TagTitle != "Bohemian Rhapsody" || got.TagArtist != "Queen" {
		t.Errorf("tag = %q, %q", got.TagTitle, got.TagArtist)
	}

	got, err = repo.Get(ctx, "job_polos")
	if err != nil {
		t.Fatal(err)
	}
	if got.TagTitle != "" || got.TagArtist != "" {
		t.Errorf("job tanpa suntingan membawa tag %q, %q", got.TagTitle, got.TagArtist)
	}
}

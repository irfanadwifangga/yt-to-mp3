package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

func TestCreateJob(t *testing.T) {
	h := newHarness(t)

	rec := h.do(t, http.MethodPost, "/api/jobs",
		`{"url":"https://youtu.be/dQw4w9WgXcQ","preset_id":"mp3_standard"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, mau 202 (body: %s)", rec.Code, rec.Body.String())
	}

	var job struct {
		ID        string           `json:"id"`
		Status    domain.JobStatus `json:"status"`
		SourceKey string           `json:"source_key"`
		Progress  *float64         `json:"progress"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if job.Status != domain.StatusQueued {
		t.Errorf("status = %s, mau queued", job.Status)
	}
	if job.SourceKey != "youtube:dQw4w9WgXcQ" {
		t.Errorf("source_key = %q", job.SourceKey)
	}
	// Progress indeterminate dikirim sebagai null, bukan 0.
	if job.Progress != nil {
		t.Errorf("progress = %v, mau null", *job.Progress)
	}
}

func TestCreateJobDitolak(t *testing.T) {
	tests := []struct {
		name string
		body string
		want domain.ErrorCode
	}{
		{"skema file", `{"url":"file:///etc/passwd"}`, domain.CodeInvalidURL},
		{"host lain", `{"url":"https://vimeo.com/1"}`, domain.CodeUnsupportedURL},
		{"preset tak dikenal", `{"url":"https://youtu.be/dQw4w9WgXcQ","preset_id":"xxx"}`, domain.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			rec := h.do(t, http.MethodPost, "/api/jobs", tc.body)
			if rec.Code < 400 {
				t.Fatalf("status = %d, mau >= 400", rec.Code)
			}
			if got := codeOf(t, rec); got != tc.want {
				t.Errorf("kode = %s, mau %s", got, tc.want)
			}
		})
	}
}

// Duplikat job aktif ditolak dengan 409, bukan diam-diam membuat dua job
// untuk sumber yang sama.
func TestCreateJobDuplikat(t *testing.T) {
	h := newHarness(t)
	body := `{"url":"https://youtu.be/dQw4w9WgXcQ"}`

	if rec := h.do(t, http.MethodPost, "/api/jobs", body); rec.Code != http.StatusAccepted {
		t.Fatalf("job pertama status = %d", rec.Code)
	}

	rec := h.do(t, http.MethodPost, "/api/jobs", body)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, mau 409", rec.Code)
	}
	if got := codeOf(t, rec); got != domain.CodeDuplicateActive {
		t.Errorf("kode = %s, mau %s", got, domain.CodeDuplicateActive)
	}
}

// Antrean penuh ditolak saat request, bukan diterima lalu menumpuk.
func TestCreateJobAntreanPenuh(t *testing.T) {
	h := newHarness(t) // kapasitas 3 pada harness

	for _, id := range []string{"aaaaaaaaaaa", "bbbbbbbbbbb", "ccccccccccc"} {
		body := `{"url":"https://youtu.be/` + id + `"}`
		if rec := h.do(t, http.MethodPost, "/api/jobs", body); rec.Code != http.StatusAccepted {
			t.Fatalf("job %s status = %d (body: %s)", id, rec.Code, rec.Body.String())
		}
	}

	rec := h.do(t, http.MethodPost, "/api/jobs", `{"url":"https://youtu.be/ddddddddddd"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, mau 503", rec.Code)
	}
	if got := codeOf(t, rec); got != domain.CodeQueueFull {
		t.Errorf("kode = %s, mau %s", got, domain.CodeQueueFull)
	}
}

func TestListDanGetJob(t *testing.T) {
	h := newHarness(t)
	h.do(t, http.MethodPost, "/api/jobs", `{"url":"https://youtu.be/dQw4w9WgXcQ"}`)

	rec := h.do(t, http.MethodGet, "/api/jobs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", rec.Code)
	}
	var list struct {
		Jobs []struct {
			ID string `json:"id"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list.Jobs) != 1 {
		t.Fatalf("jumlah job = %d, mau 1", len(list.Jobs))
	}

	got := h.do(t, http.MethodGet, "/api/jobs/"+list.Jobs[0].ID, "")
	if got.Code != http.StatusOK {
		t.Errorf("status = %d, mau 200", got.Code)
	}

	missing := h.do(t, http.MethodGet, "/api/jobs/job_tidak_ada", "")
	if missing.Code != http.StatusNotFound {
		t.Errorf("status = %d, mau 404", missing.Code)
	}
}

// Handler hanya memicu pembatalan; yang menulis status terminal adalah
// goroutine pemilik job setelah prosesnya benar-benar berhenti.
func TestCancelJobMemicuScheduler(t *testing.T) {
	h := newHarness(t)
	h.do(t, http.MethodPost, "/api/jobs", `{"url":"https://youtu.be/dQw4w9WgXcQ"}`)

	var created struct {
		Jobs []struct {
			ID string `json:"id"`
		} `json:"jobs"`
	}
	list := h.do(t, http.MethodGet, "/api/jobs", "")
	if err := json.Unmarshal(list.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id := created.Jobs[0].ID

	rec := h.do(t, http.MethodPost, "/api/jobs/"+id+"/cancel", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, mau 202 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(h.canceller.cancelled) != 1 || h.canceller.cancelled[0] != id {
		t.Errorf("scheduler menerima %v, mau [%s]", h.canceller.cancelled, id)
	}
}

func TestDeleteJob(t *testing.T) {
	h := newHarness(t)
	h.do(t, http.MethodPost, "/api/jobs", `{"url":"https://youtu.be/dQw4w9WgXcQ"}`)

	var list struct {
		Jobs []struct {
			ID string `json:"id"`
		} `json:"jobs"`
	}
	_ = json.Unmarshal(h.do(t, http.MethodGet, "/api/jobs", "").Body.Bytes(), &list)
	id := list.Jobs[0].ID

	// Job yang masih aktif harus dibatalkan lebih dulu, bukan dihapus
	// begitu saja sambil meninggalkan proses berjalan.
	if rec := h.do(t, http.MethodDelete, "/api/jobs/"+id, ""); rec.Code < 400 {
		t.Errorf("hapus job aktif status = %d, mau >= 400", rec.Code)
	}

	if err := h.jobs.Transition(context.Background(), id,
		domain.StatusQueued, domain.StatusCancelled, domain.Event{}); err != nil {
		t.Fatalf("tutup job: %v", err)
	}
	if rec := h.do(t, http.MethodDelete, "/api/jobs/"+id, ""); rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, mau 204", rec.Code)
	}
}

// Job terminal tidak boleh menggantungkan koneksi: pelanggan menerima
// histori beserta state akhirnya, lalu stream ditutup.
func TestSSEJobTerminalDitutup(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	job := &domain.Job{
		ID: "job_selesai", SourceKey: "youtube:aaa", SourceURL: "https://x.test",
		Status: domain.StatusQueued, PresetID: "mp3_standard", FilenameMode: domain.FilenameTitle,
	}
	if err := h.jobs.Create(ctx, job); err != nil {
		t.Fatalf("buat job: %v", err)
	}
	if err := h.jobs.Transition(ctx, job.ID, domain.StatusQueued, domain.StatusCancelled,
		domain.Event{Type: domain.EventDone, Payload: "cancelled"}); err != nil {
		t.Fatalf("tutup job: %v", err)
	}

	done := make(chan string, 1)
	go func() {
		done <- h.do(t, http.MethodGet, "/api/jobs/"+job.ID+"/events", "").Body.String()
	}()

	select {
	case body := <-done:
		if !strings.Contains(body, "event: done") {
			t.Errorf("stream tidak memuat event done:\n%s", body)
		}
		// Event terpersist wajib membawa id supaya resume berfungsi.
		if !strings.Contains(body, "id: 1") {
			t.Errorf("stream tidak memuat id event:\n%s", body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream job terminal tidak pernah ditutup")
	}
}

// Urutan berlangganan menentukan ada tidaknya event yang hilang: listener
// harus terpasang sebelum histori dibaca.
func TestHubTidakKehilanganEvent(t *testing.T) {
	repo := newFakeJobRepo()
	hub := api.NewHub(repo, newDiscardLogger())
	ctx := context.Background()

	events, unsubscribe, err := hub.Subscribe(ctx, "job_1", 0, false)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	hub.Publish(application.StreamEvent{
		JobID: "job_1", Seq: 1, Type: application.StreamState,
		Status: domain.StatusDownloading,
	})

	select {
	case ev := <-events:
		if ev.Status != domain.StatusDownloading {
			t.Errorf("status = %s, mau downloading", ev.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("event tidak pernah sampai")
	}
}

// Progress bersifat lossy dan di-throttle; hanya nilai terbaru yang berguna.
func TestHubMenyaringProgressBerlebihan(t *testing.T) {
	repo := newFakeJobRepo()
	hub := api.NewHub(repo, newDiscardLogger())

	events, unsubscribe, err := hub.Subscribe(context.Background(), "job_1", 0, false)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer unsubscribe()

	for i := range 20 {
		pct := float64(i)
		hub.Publish(application.StreamEvent{
			JobID: "job_1", Type: application.StreamProgress, Percent: &pct,
		})
	}

	time.Sleep(50 * time.Millisecond)
	var received int
	for {
		select {
		case <-events:
			received++
			continue
		default:
		}
		break
	}

	if received == 0 {
		t.Error("tidak ada progress yang diteruskan")
	}
	if received > 3 {
		t.Errorf("%d event progress diteruskan dalam sekejap, throttle tidak bekerja", received)
	}
}

func TestHubMembatasiJumlahPelanggan(t *testing.T) {
	repo := newFakeJobRepo()
	hub := api.NewHub(repo, newDiscardLogger())
	ctx := context.Background()

	for i := range 4 {
		if _, _, err := hub.Subscribe(ctx, "job_1", 0, false); err != nil {
			t.Fatalf("pelanggan %d ditolak: %v", i, err)
		}
	}
	if _, _, err := hub.Subscribe(ctx, "job_1", 0, false); err == nil {
		t.Error("pelanggan kelima seharusnya ditolak")
	}
}

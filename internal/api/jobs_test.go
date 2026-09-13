package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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

// UI mengambil antrean dan riwayat terpisah; riwayat panjang tidak boleh
// mendesak job aktif keluar dari halaman.
func TestListJobsPerKelompok(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	for _, j := range []*domain.Job{
		{ID: "job_antre", SourceKey: "youtube:aaa", Status: domain.StatusQueued, PresetID: "mp3_standard"},
		{ID: "job_selesai", SourceKey: "youtube:bbb", Status: domain.StatusCompleted, PresetID: "mp3_standard"},
	} {
		if err := h.jobs.Create(ctx, j); err != nil {
			t.Fatal(err)
		}
	}

	for status, want := range map[string]string{"active": "job_antre", "finished": "job_selesai"} {
		rec := h.do(t, http.MethodGet, "/api/jobs?status="+status, "")
		var list struct {
			Jobs []struct {
				ID string `json:"id"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(list.Jobs) != 1 || list.Jobs[0].ID != want {
			t.Errorf("status=%s -> %+v, mau [%s]", status, list.Jobs, want)
		}
	}

	if rec := h.do(t, http.MethodGet, "/api/jobs?status=entah", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("status tak dikenal = %d, mau 400", rec.Code)
	}
}

func TestDeleteJobBesertaBerkas(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	dir := t.TempDir()

	add := func(id string) string {
		t.Helper()
		if err := h.jobs.Create(ctx, &domain.Job{
			ID: id, SourceKey: "youtube:" + id, Status: domain.StatusCompleted, PresetID: "mp3_standard",
		}); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, id+".mp3")
		if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.files.add(&domain.File{ID: "file_" + id, JobID: id, Path: path, Filename: id + ".mp3"})
		return path
	}

	kept := add("job_simpan")
	removed := add("job_buang")

	if rec := h.do(t, http.MethodDelete, "/api/jobs/job_simpan?delete_file=false", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("hapus tanpa berkas = %d", rec.Code)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("berkas ikut terhapus walau tidak diminta: %v", err)
	}

	if rec := h.do(t, http.MethodDelete, "/api/jobs/job_buang?delete_file=true", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("hapus beserta berkas = %d (body: %s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(removed); !os.IsNotExist(err) {
		t.Errorf("berkas masih ada setelah delete_file=true: %v", err)
	}
	if _, err := h.jobs.Get(ctx, "job_buang"); err == nil {
		t.Error("job masih ada setelah dihapus")
	}
}

// Stream untuk job yang belum terminal tidak pernah selesai sendiri. Tanpa
// penutupan saat shutdown, keluar selagi ada job di antrean menunggu sampai
// batas waktu shutdown lalu gagal.
func TestSSEDitutupSaatShutdown(t *testing.T) {
	h := newHarness(t)
	if err := h.jobs.Create(context.Background(), &domain.Job{
		ID: "job_antre", SourceKey: "youtube:aaa", SourceURL: "https://x.test",
		Status: domain.StatusQueued, PresetID: "mp3_standard", FilenameMode: domain.FilenameTitle,
	}); err != nil {
		t.Fatalf("buat job: %v", err)
	}

	done := make(chan struct{})
	go func() {
		h.do(t, http.MethodGet, "/api/jobs/job_antre/events", "")
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("stream job aktif selesai sebelum shutdown")
	case <-time.After(200 * time.Millisecond):
	}

	h.srv.CloseStreams()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream tidak ditutup setelah CloseStreams")
	}
}

// Analisis memberi tahu bila video yang sama pernah dikonversi, supaya
// menekan Konversi lagi tidak diam-diam menghasilkan "Judul (2).mp3".
func TestMetadataMenyertakanKonversiSebelumnya(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	finished := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)

	for _, j := range []*domain.Job{
		{ID: "job_ada", SourceKey: "youtube:dQw4w9WgXcQ", Status: domain.StatusCompleted, PresetID: "mp3_standard", FinishedAt: &finished},
		{ID: "job_berkas_hilang", SourceKey: "youtube:dQw4w9WgXcQ", Status: domain.StatusCompleted, PresetID: "mp3_high", FinishedAt: &finished},
		{ID: "job_gagal", SourceKey: "youtube:dQw4w9WgXcQ", Status: domain.StatusFailed, PresetID: "mp3_max"},
		{ID: "job_video_lain", SourceKey: "youtube:aaaaaaaaaaa", Status: domain.StatusCompleted, PresetID: "mp3_standard"},
	} {
		if err := h.jobs.Create(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	h.files.add(&domain.File{ID: "file_ada", JobID: "job_ada", Filename: "Judul Contoh.mp3"})
	h.files.add(&domain.File{ID: "file_lain", JobID: "job_video_lain", Filename: "Lain.mp3"})

	rec := h.do(t, http.MethodPost, "/api/metadata", `{"url":"https://youtu.be/dQw4w9WgXcQ"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Previous []struct {
			JobID    string `json:"job_id"`
			PresetID string `json:"preset_id"`
			FileID   string `json:"file_id"`
			FileName string `json:"file_name"`
		} `json:"previous_conversions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Previous) != 1 {
		t.Fatalf("konversi sebelumnya = %+v, mau hanya job_ada", body.Previous)
	}
	got := body.Previous[0]
	if got.JobID != "job_ada" || got.PresetID != "mp3_standard" || got.FileID != "file_ada" || got.FileName != "Judul Contoh.mp3" {
		t.Errorf("konversi sebelumnya = %+v", got)
	}
}

// Judul dan artis suntingan diteruskan ke job dan dipakai sebagai judul
// riwayat.
func TestCreateJobDenganTag(t *testing.T) {
	h := newHarness(t)

	rec := h.do(t, http.MethodPost, "/api/jobs",
		`{"url":"https://youtu.be/dQw4w9WgXcQ","preset_id":"mp3_standard","title":"  Judul\nBaru ","artist":"Artis"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (body: %s)", rec.Code, rec.Body.String())
	}
	var view struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Title != "Judul Baru" {
		t.Errorf("title = %q, mau judul suntingan yang dirapikan", view.Title)
	}

	h.jobs.mu.Lock()
	job := h.jobs.jobs[view.ID]
	h.jobs.mu.Unlock()
	if job == nil || job.TagTitle != "Judul Baru" || job.TagArtist != "Artis" {
		t.Errorf("job tersimpan = %+v", job)
	}
}

func TestCreateJobTagTerlaluPanjang(t *testing.T) {
	h := newHarness(t)
	long := strings.Repeat("a", domain.MaxTagRunes+1)

	rec := h.do(t, http.MethodPost, "/api/jobs",
		`{"url":"https://youtu.be/dQw4w9WgXcQ","title":"`+long+`"}`)
	if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "BAD_REQUEST" {
		t.Errorf("status = %d kode = %s, mau 400 BAD_REQUEST", rec.Code, codeOf(t, rec))
	}
}

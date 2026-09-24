package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// updateGolden menulis ulang berkas golden dari respons saat ini:
//
//	go test ./internal/api/ -run Kontrak -update
//
// Diff berkas golden adalah perubahan kontrak API. Tinjau sebelum commit,
// karena SPA dan klien lain membaca bentuk yang sama.
var updateGolden = flag.Bool("update", false, "tulis ulang berkas golden kontrak API")

// volatile adalah field level atas yang nilainya berubah setiap run atau
// setiap build, dan karena itu bukan bagian dari kontrak bentuk respons.
// Hanya level atas yang dinormalkan: versi tool di dalam daftar tool adalah
// data tetap dari fake dan justru termasuk kontrak.
var volatile = map[string]any{
	"uptime_seconds": 0,
	"version":        "<version>",
	"commit":         "<commit>",
}

// normalize mengurai JSON, mengganti field volatil level atas, lalu menulis
// ulang dengan kunci terurut dan indentasi tetap supaya diff golden stabil.
func normalize(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("respons bukan JSON: %v\n%s", err, raw)
	}
	if top, ok := v.(map[string]any); ok {
		for k, replacement := range volatile {
			if _, present := top[k]; present {
				top[k] = replacement
			}
		}
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // golden harus terbaca apa adanya saat direview
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertGolden(t *testing.T, name string, status, wantStatus int, body []byte) {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("%s: status = %d, mau %d (body: %s)", name, status, wantStatus, body)
	}

	got := normalize(t, body)
	path := filepath.Join("testdata", "golden", name+".json")

	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s belum ada; jalankan dengan -update: %v", path, err)
	}
	// Normalisasi akhir baris: checkout Windows dapat mengubah LF menjadi CRLF.
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Errorf("kontrak %s berubah.\n--- golden\n%s\n--- sekarang\n%s\nBila disengaja, jalankan ulang dengan -update dan tinjau diff-nya.",
			name, want, got)
	}
}

// seedContractJobs mengisi repository dengan job bertimestamp tetap yang
// mewakili setiap bentuk: selesai dengan berkas, gagal, dan menunggu
// auto-retry.
func seedContractJobs(t *testing.T, h *harness) {
	t.Helper()
	at := func(minute int) time.Time { return time.Date(2026, 9, 13, 10, minute, 0, 0, time.UTC) }
	ptr := func(v time.Time) *time.Time { return &v }
	progress := 100.0

	jobs := []*domain.Job{
		{
			ID: "job_selesai", SourceURL: "https://www.youtube.com/watch?v=aaaaaaaaaaa",
			SourceKey: "youtube:aaaaaaaaaaa", Title: "Lagu Selesai", Status: domain.StatusCompleted,
			PresetID: "mp3_standard", FilenameMode: domain.FilenameTitle, Progress: &progress,
			CreatedAt: at(0), StartedAt: ptr(at(1)), FinishedAt: ptr(at(2)),
		},
		{
			ID: "job_gagal", SourceURL: "https://www.youtube.com/watch?v=bbbbbbbbbbb",
			SourceKey: "youtube:bbbbbbbbbbb", Title: "Lagu Privat", Status: domain.StatusFailed,
			PresetID: "mp3_standard", FilenameMode: domain.FilenameTitle,
			ErrorCode: domain.CodeVideoPrivate, ErrorMessage: "pesan mentah yt-dlp",
			CreatedAt: at(3), StartedAt: ptr(at(4)), FinishedAt: ptr(at(5)),
		},
		{
			ID: "job_menunggu", SourceURL: "https://www.youtube.com/watch?v=ccccccccccc",
			SourceKey: "youtube:ccccccccccc", Title: "Lagu Menunggu", Status: domain.StatusQueued,
			PresetID: "mp3_high", FilenameMode: domain.FilenameTitleUploader,
			AttemptCount: 1, ErrorCode: domain.CodeRateLimited, RetryAt: ptr(at(40)),
			CreatedAt: at(6), StartedAt: ptr(at(7)),
		},
	}
	for _, j := range jobs {
		if err := h.jobs.Create(context.Background(), j); err != nil {
			t.Fatalf("buat %s: %v", j.ID, err)
		}
	}
	h.files.add(&domain.File{
		ID: "file_selesai", JobID: "job_selesai", Filename: "Lagu Selesai.mp3",
		Path: "C:/tidak/boleh/bocor/Lagu Selesai.mp3",
	})
}

// Bentuk respons yang dibaca SPA dikunci berkas golden. Pengecekan field per
// field di test lain menangkap perilaku; test ini menangkap perubahan bentuk
// yang tidak disengaja, seperti field yang berganti nama, hilang, atau path
// filesystem yang bocor.
func TestKontrakAPI(t *testing.T) {
	h := newHarness(t)
	seedContractJobs(t, h)
	h.tools.status["yt-dlp"] = application.ToolStatus{
		Name: "yt-dlp", Available: true, Version: "2026.08.19", Source: "managed",
		Pinned: "2026.08.19", Latest: "2026.09.01", UpdateAvailable: true, Updatable: true,
		Path: "C:/tidak/boleh/bocor.exe",
	}

	cases := []struct {
		name, method, path, body string
		status                   int
	}{
		{"jobs_list", http.MethodGet, "/api/jobs", "", http.StatusOK},
		{"jobs_list_active", http.MethodGet, "/api/jobs?status=active", "", http.StatusOK},
		{"job_get", http.MethodGet, "/api/jobs/job_selesai", "", http.StatusOK},
		{"error_not_found", http.MethodGet, "/api/jobs/job_tidak_ada", "", http.StatusNotFound},
		{"error_invalid_url", http.MethodPost, "/api/metadata", `{"url":"file:///etc/passwd"}`, http.StatusBadRequest},
		{"metadata", http.MethodPost, "/api/metadata", `{"url":"https://youtu.be/dQw4w9WgXcQ"}`, http.StatusOK},
		{"tools", http.MethodGet, "/api/tools", "", http.StatusOK},
		{"presets", http.MethodGet, "/api/presets", "", http.StatusOK},
		{"health", http.MethodGet, "/api/health", "", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(t, tc.method, tc.path, tc.body)
			assertGolden(t, tc.name, rec.Code, tc.status, rec.Body.Bytes())
			// Path filesystem tidak pernah boleh menyeberang batas API,
			// apa pun isi golden-nya.
			if bytes.Contains(rec.Body.Bytes(), []byte("bocor")) {
				t.Errorf("respons %s membocorkan path filesystem", tc.name)
			}
		})
	}
}

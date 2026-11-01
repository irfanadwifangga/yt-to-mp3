package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
)

func TestTrimRequestAndRetry(t *testing.T) {
	h := newHarness(t)
	h.resolver.info.DurationMS = 10000
	for _, body := range []string{
		`{"url":"https://youtu.be/dQw4w9WgXcQ","trim_start_ms":0}`,
		`{"url":"https://youtu.be/dQw4w9WgXcQ","trim_start_ms":3000,"trim_end_ms":2000}`,
		`{"url":"https://youtu.be/dQw4w9WgXcQ","trim_start_ms":0,"trim_end_ms":10001}`,
	} {
		rec := h.do(t, http.MethodPost, "/api/jobs", body)
		if rec.Code != http.StatusBadRequest || codeOf(t, rec) != domain.CodeInvalidTrim {
			t.Fatalf("invalid trim accepted: %d %s", rec.Code, rec.Body.String())
		}
	}
	rec := h.do(t, http.MethodPost, "/api/jobs", `{"url":"https://youtu.be/dQw4w9WgXcQ","trim_start_ms":1250,"trim_end_ms":3500}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if err := h.jobs.Transition(context.Background(), created.ID, domain.StatusQueued, domain.StatusCancelled, domain.Event{}); err != nil {
		t.Fatal(err)
	}
	rec = h.do(t, http.MethodPost, "/api/jobs/"+created.ID+"/retry", "")
	var retry struct {
		Start    int64 `json:"trim_start_ms"`
		End      int64 `json:"trim_end_ms"`
		Duration int64 `json:"duration_ms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &retry); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusAccepted || retry.Start != 1250 || retry.End != 3500 || retry.Duration != 2250 {
		t.Fatalf("retry lost trim: %d %s", rec.Code, rec.Body.String())
	}
}

func TestNumberedPageAPIWithDatabase(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	log := newDiscardLogger()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "pages.db"), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(ctx, "test"); err != nil {
		t.Fatal(err)
	}
	repo := db.NewJobRepository(d)
	hub := api.NewHub(repo, log)
	service := application.NewJobService(repo, h.presets, h.cache, h.resolver, h.files, hub, nil, func() application.LiveSettings { return application.LiveSettings{MaxQueueDepth: 50} }, log)
	h.srv = api.New(api.Options{Config: config.Default(), Logger: log, Token: testToken, Port: testPort, Jobs: service, Files: h.files})
	for i := 0; i < 23; i++ {
		j := &domain.Job{ID: fmt.Sprintf("job_%02d", i), SourceURL: "https://youtu.be/dQw4w9WgXcQ", SourceKey: fmt.Sprintf("source_%02d", i), Title: fmt.Sprintf("Recording %02d", i), Status: domain.StatusCompleted, PresetID: "mp3_standard", FilenameMode: domain.FilenameTitle}
		if err := repo.Create(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	rec := h.do(t, http.MethodGet, "/api/jobs?page=3&page_size=10&kind=audio", "")
	var page struct {
		Jobs  []json.RawMessage `json:"jobs"`
		Total int               `json:"total"`
		Page  int               `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || page.Total != 23 || page.Page != 3 || len(page.Jobs) != 3 {
		t.Fatalf("page: %d %s", rec.Code, rec.Body.String())
	}
	rec = h.do(t, http.MethodGet, "/api/jobs?page=1&page_size=10&q=Recording%2000&status=completed", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Jobs) != 1 {
		t.Fatalf("search: %s", rec.Body.String())
	}
	for _, query := range []string{"page=0", "page=1&page_size=0", "page=1&kind=invalid", "page=1&cursor=abc"} {
		if rec := h.do(t, http.MethodGet, "/api/jobs?"+query, ""); rec.Code != http.StatusBadRequest {
			t.Fatalf("query %s got %d", query, rec.Code)
		}
	}
}

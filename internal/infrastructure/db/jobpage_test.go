package db_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
)

func TestTrimPersistenceAndIndependentDeduplication(t *testing.T) {
	d := migrated(t)
	r := db.NewJobRepository(d)
	ctx := context.Background()
	start, end, otherEnd := int64(1000), int64(3000), int64(4000)
	job := newJob("trim_one", "same_source")
	job.TrimStartMS, job.TrimEndMS = &start, &end
	if err := r.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.TrimStartMS == nil || *got.TrimStartMS != start || got.TrimEndMS == nil || *got.TrimEndMS != end {
		t.Fatalf("range lost: %+v", got)
	}
	job.ID = "trim_duplicate"
	wantDomainCode(t, r.Create(ctx, job), domain.CodeDuplicateActive)
	job.ID, job.TrimEndMS = "trim_other", &otherEnd
	if err := r.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	job.ID, job.TrimStartMS, job.TrimEndMS = "full_source", nil, nil
	if err := r.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
}

func TestNumberedPagesSearchWholeHistory(t *testing.T) {
	d := migrated(t)
	r := db.NewJobRepository(d)
	ctx := context.Background()
	for i := 0; i < 23; i++ {
		j := newJob(fmt.Sprintf("page_%02d", i), fmt.Sprintf("source_%02d", i))
		j.Title = fmt.Sprintf("Recording %02d", i)
		if i == 0 {
			j.Title = "A literal 100%_ result"
		}
		if err := r.Create(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	page, err := r.ListPage(ctx, application.JobListQuery{Page: 3, PageSize: 10, Kind: domain.KindAudio})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 23 || len(page.Jobs) != 3 || page.Page != 3 {
		t.Fatalf("bad page: %+v", page)
	}
	page, err = r.ListPage(ctx, application.JobListQuery{Page: 1, PageSize: 10, Query: "100%_"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Jobs[0].ID != "page_00" {
		t.Fatalf("search did not reach old history: %+v", page)
	}
	page, err = r.ListPage(ctx, application.JobListQuery{Page: 99, PageSize: 10, Status: domain.StatusCompleted})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.Jobs) != 0 || page.Page != 1 {
		t.Fatalf("empty page not clamped: %+v", page)
	}
}

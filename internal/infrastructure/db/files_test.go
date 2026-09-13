package db_test

import (
	"context"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
)

// Penanda hilang harus bisa dibalik: berkas di drive eksternal yang dicabut
// lalu dicolok kembali kembali tersedia di riwayat.
func TestFileSetMissingDuaArah(t *testing.T) {
	d := migrated(t)
	jobs := db.NewJobRepository(d)
	files := db.NewFileRepository(d)
	ctx := context.Background()

	if err := jobs.Create(ctx, newJob("job_1", "abc")); err != nil {
		t.Fatalf("Create job: %v", err)
	}
	if err := files.Create(ctx, &domain.File{
		ID: "file_1", JobID: "job_1", Path: "E:/musik/lagu.mp3", Filename: "lagu.mp3",
		MIME: "audio/mpeg", SizeBytes: 1024, SHA256: "abc",
	}); err != nil {
		t.Fatalf("Create file: %v", err)
	}

	check := func(wantMissing bool) {
		t.Helper()
		f, err := files.Get(ctx, "file_1")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if f.Missing != wantMissing {
			t.Errorf("Missing = %v, mau %v", f.Missing, wantMissing)
		}
		ids, err := files.RefsByJobs(ctx, []string{"job_1"})
		if err != nil {
			t.Fatalf("RefsByJobs() error = %v", err)
		}
		if _, listed := ids["job_1"]; listed == wantMissing {
			t.Errorf("tercantum di riwayat = %v, mau %v", listed, !wantMissing)
		}
	}

	if err := files.SetMissing(ctx, "file_1", true); err != nil {
		t.Fatalf("SetMissing(true) error = %v", err)
	}
	check(true)

	if err := files.SetMissing(ctx, "file_1", false); err != nil {
		t.Fatalf("SetMissing(false) error = %v", err)
	}
	check(false)
}

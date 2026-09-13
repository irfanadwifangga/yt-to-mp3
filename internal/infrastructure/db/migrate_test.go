package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
	"github.com/irfanadwifangga/yt-to-mp3/migrations"
)

// Pengguna yang memperbarui aplikasi membawa database yang dibuat versi
// sebelumnya. Migrasi harus menaikkan skema tanpa kehilangan riwayat, dan
// itu hanya terbukti dengan memulai dari snapshot skema lama, bukan dari
// database kosong.
func TestMigrateDariSkemaVersi1(t *testing.T) {
	ctx := context.Background()
	d := openTemp(t)

	provider, err := goose.NewProvider(goose.DialectSQLite3, d.Write(), migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatalf("UpTo(1) error = %v", err)
	}

	// Isi snapshot v1 lewat SQL mentah, bukan repository: repository selalu
	// mengikuti skema terbaru, sedangkan yang diuji justru data lama.
	for _, stmt := range []string{
		`INSERT INTO jobs (id, source_url, source_key, title, status, preset_id, filename_mode,
		                   attempt_count, created_at, finished_at)
		 VALUES ('job_lama', 'https://www.youtube.com/watch?v=aaaaaaaaaaa', 'youtube:aaaaaaaaaaa',
		         'Lagu lama', 'completed', 'mp3_standard', 'title', 1,
		         '2026-01-01T10:00:00Z', '2026-01-01T10:01:00Z')`,
		`INSERT INTO jobs (id, source_url, source_key, title, status, preset_id, filename_mode,
		                   attempt_count, created_at, finished_at)
		 VALUES ('job_baru', 'https://www.youtube.com/watch?v=bbbbbbbbbbb', 'youtube:bbbbbbbbbbb',
		         'Lagu baru', 'completed', 'mp3_standard', 'title', 1,
		         '2026-02-01T10:00:00Z', '2026-02-01T10:01:00Z')`,
		`INSERT INTO job_events (job_id, seq, type, payload, created_at)
		 VALUES ('job_lama', 1, 'done', '{}', '2026-01-01T10:01:00Z')`,
		`INSERT INTO files (id, job_id, path, filename, mime, size_bytes, sha256, created_at)
		 VALUES ('file_lama', 'job_lama', 'C:/musik/lama.mp3', 'lama.mp3', 'audio/mpeg', 1024, 'abc',
		         '2026-01-01T10:01:00Z')`,
	} {
		if _, err := d.Write().ExecContext(ctx, stmt); err != nil {
			t.Fatalf("isi snapshot v1: %v", err)
		}
	}

	if err := d.Migrate(ctx, "0.0.0-test"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var version int
	if err := d.Read().QueryRowContext(ctx,
		`SELECT schema_version FROM schema_meta WHERE id = 1`).Scan(&version); err != nil {
		t.Fatalf("baca schema_version: %v", err)
	}
	if version != db.SchemaVersion {
		t.Errorf("schema_version = %d, mau %d", version, db.SchemaVersion)
	}

	jobs, _, err := db.NewJobRepository(d).List(ctx, application.JobListQuery{
		Status: domain.StatusCompleted, Limit: 10,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(jobs) != 2 || jobs[0].ID != "job_baru" || jobs[1].ID != "job_lama" {
		ids := make([]string, len(jobs))
		for i, j := range jobs {
			ids[i] = j.ID
		}
		t.Errorf("riwayat setelah migrasi = %v, mau [job_baru job_lama]", ids)
	}

	if _, err := db.NewFileRepository(d).Get(ctx, "file_lama"); err != nil {
		t.Errorf("berkas lama hilang setelah migrasi: %v", err)
	}
	var events int
	_ = d.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM job_events`).Scan(&events)
	if events != 1 {
		t.Errorf("event = %d, mau 1", events)
	}
}

// Riwayat berfilter status harus dilayani indeks tanpa sort sementara.
// Rencana query diperiksa langsung karena regresi ini tidak terlihat pada
// database kecil di test, hanya pada riwayat ribuan job di mesin pengguna.
func TestRiwayatBerfilterStatusMemakaiIndeks(t *testing.T) {
	ctx := context.Background()
	d := migrated(t)

	var indexes []string
	rows, err := d.Read().QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'jobs'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var name string
		_ = rows.Scan(&name)
		indexes = append(indexes, name)
	}
	_ = rows.Close()

	has := func(name string) bool {
		for _, n := range indexes {
			if n == name {
				return true
			}
		}
		return false
	}
	if !has("idx_jobs_status_created") {
		t.Errorf("indeks idx_jobs_status_created tidak ada; indeks: %v", indexes)
	}
	if has("idx_jobs_status") {
		t.Error("idx_jobs_status seharusnya sudah dibuang")
	}

	plan, err := d.Read().QueryContext(ctx, `EXPLAIN QUERY PLAN
		SELECT id FROM jobs WHERE status = 'completed'
		ORDER BY created_at DESC, id DESC LIMIT 51`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = plan.Close() }()

	var steps []string
	for plan.Next() {
		var id, parent, notused int
		var detail string
		if err := plan.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatal(err)
		}
		steps = append(steps, detail)
	}
	joined := strings.Join(steps, " | ")
	if !strings.Contains(joined, "idx_jobs_status_created") {
		t.Errorf("rencana query tidak memakai indeks komposit: %s", joined)
	}
	if strings.Contains(joined, "TEMP B-TREE") {
		t.Errorf("rencana query masih mengurutkan di memori: %s", joined)
	}
}

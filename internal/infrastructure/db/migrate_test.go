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

	// Preset yang dirujuk riwayat lama tetap preset audio setelah kolom
	// kind ditambahkan.
	preset, err := db.NewPresetRepository(d).Get(ctx, "mp3_standard")
	if err != nil {
		t.Fatalf("baca preset lama: %v", err)
	}
	if preset.IsVideo() || preset.MaxHeight != nil {
		t.Errorf("preset lama = %+v, mau audio tanpa max_height", preset)
	}
}

// Migrasi 00006 dan 00007 bisa dibalik tanpa menyisakan preset baru maupun
// kolomnya, lalu diterapkan lagi dengan hasil yang sama.
func TestMigrasiPresetVideoBisaDibalik(t *testing.T) {
	ctx := context.Background()
	d := openTemp(t)

	provider, err := goose.NewProvider(goose.DialectSQLite3, d.Write(), migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if _, err := provider.DownTo(ctx, 5); err != nil {
		t.Fatalf("DownTo(5) error = %v", err)
	}

	var count int
	if err := d.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM presets`).Scan(&count); err != nil {
		t.Fatalf("hitung preset: %v", err)
	}
	if count != 5 {
		t.Errorf("jumlah preset setelah Down = %d, mau 5", count)
	}
	if _, err := d.Read().ExecContext(ctx, `SELECT kind FROM presets`); err == nil {
		t.Error("kolom kind masih ada setelah Down")
	}

	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatalf("UpTo(6) ulang error = %v", err)
	}
	if err := d.Read().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM presets WHERE kind = 'video'`).Scan(&count); err != nil {
		t.Fatalf("hitung preset video: %v", err)
	}
	if count != 5 {
		t.Errorf("preset video pada versi 6 = %d, mau 5", count)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("Up() ulang error = %v", err)
	}
	if err := d.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM presets`).Scan(&count); err != nil {
		t.Fatalf("hitung preset: %v", err)
	}
	if count != 42 {
		t.Errorf("preset setelah Up ulang = %d, mau 42", count)
	}
}

// Membangun ulang tabel presets tidak boleh memutus riwayat: job lama tetap
// merujuk presetnya, dan foreign key kembali ditegakkan sesudahnya.
func TestMigrasiFormatMenjagaRiwayat(t *testing.T) {
	ctx := context.Background()
	d := openTemp(t)

	provider, err := goose.NewProvider(goose.DialectSQLite3, d.Write(), migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if _, err := provider.UpTo(ctx, 6); err != nil {
		t.Fatalf("UpTo(6) error = %v", err)
	}
	if _, err := d.Write().ExecContext(ctx, `INSERT INTO jobs
		(id, source_url, source_key, title, status, preset_id, created_at)
		VALUES ('job_video', 'https://www.youtube.com/watch?v=aaaaaaaaaaa', 'youtube:aaaaaaaaaaa',
		        'Video lama', 'completed', 'mp4_720', '2026-09-20T10:00:00Z')`); err != nil {
		t.Fatalf("isi job: %v", err)
	}

	if err := d.Migrate(ctx, "0.0.0-test"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	job, err := db.NewJobRepository(d).Get(ctx, "job_video")
	if err != nil {
		t.Fatalf("job lama hilang: %v", err)
	}
	preset, err := db.NewPresetRepository(d).Get(ctx, job.PresetID)
	if err != nil || !preset.IsVideo() || !preset.Passthrough {
		t.Errorf("preset job lama = %+v, %v; mau video dengan passthrough", preset, err)
	}

	var fk int
	if err := d.Write().QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v; mau 1 setelah migrasi", fk, err)
	}
	if _, err := d.Write().ExecContext(ctx, `INSERT INTO jobs
		(id, source_url, source_key, status, preset_id, created_at)
		VALUES ('job_yatim', 'x', 'youtube:bbbbbbbbbbb', 'queued', 'tidak_ada', '2026-09-20T10:00:00Z')`); err == nil {
		t.Error("job dengan preset yang tidak ada seharusnya ditolak foreign key")
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

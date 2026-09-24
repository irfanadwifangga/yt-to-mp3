package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// openTemp membuka database di direktori sementara yang namanya sengaja
// memuat spasi.
//
// Direktori data Windows lazim berada di bawah nama pengguna yang berspasi
// (misalnya "C:\Users\Nama Depan\AppData\..."), dan spasi mentah merusak
// parsing DSN berbentuk URI. Tanpa test ini, kegagalannya baru muncul di
// mesin pengguna, bukan di CI.
func openTemp(t *testing.T) *db.DB {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "data dir dengan spasi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("buat dir: %v", err)
	}

	d, err := db.Open(context.Background(), filepath.Join(dir, "app.db"), discardLogger())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func migrated(t *testing.T) *db.DB {
	t.Helper()
	d := openTemp(t)
	if err := d.Migrate(context.Background(), "0.0.0-test"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return d
}

func TestMigrateDatabaseKosong(t *testing.T) {
	d := migrated(t)

	want := []string{
		"jobs", "media_items", "files", "job_events",
		"presets", "settings", "schema_meta",
	}
	for _, table := range want {
		var n int
		err := d.Read().QueryRow(
			`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
		).Scan(&n)
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		if n != 1 {
			t.Errorf("tabel %s tidak dibuat", table)
		}
	}
}

func TestMigrateIdempoten(t *testing.T) {
	d := migrated(t)

	// Startup kedua harus melewati migrasi tanpa error dan tanpa mengubah
	// apa pun.
	if err := d.Migrate(context.Background(), "0.0.0-test"); err != nil {
		t.Fatalf("Migrate() kedua error = %v", err)
	}

	var version int
	if err := d.Read().QueryRow(`SELECT schema_version FROM schema_meta WHERE id = 1`).
		Scan(&version); err != nil {
		t.Fatalf("baca schema_version: %v", err)
	}
	if version != db.SchemaVersion {
		t.Errorf("schema_version = %d, mau %d", version, db.SchemaVersion)
	}
}

// Aplikasi lama di atas database yang lebih baru harus menolak sejak awal,
// bukan gagal di tengah migrasi dengan pesan menyesatkan.
func TestMigrateMenolakSkemaLebihBaru(t *testing.T) {
	d := migrated(t)

	if _, err := d.Write().Exec(
		`UPDATE schema_meta SET schema_version = ? WHERE id = 1`, db.SchemaVersion+1,
	); err != nil {
		t.Fatalf("naikkan schema_version: %v", err)
	}

	err := d.Migrate(context.Background(), "0.0.0-test")
	if err == nil {
		t.Fatal("Migrate() seharusnya menolak skema yang lebih baru")
	}
	if !strings.Contains(err.Error(), "lebih baru") {
		t.Errorf("pesan error tidak menjelaskan sebabnya: %v", err)
	}
}

func TestSeedPreset(t *testing.T) {
	d := migrated(t)

	var count int
	if err := d.Read().QueryRow(`SELECT count(*) FROM presets`).Scan(&count); err != nil {
		t.Fatalf("hitung preset: %v", err)
	}
	if count != 42 {
		t.Errorf("jumlah preset = %d, mau 42", count)
	}

	// 48 kHz adalah keputusan sadar yang menyesuaikan sumber Opus (ADR-030);
	// kalau seed-nya bergeser diam-diam, seluruh output ikut berubah.
	rows, err := d.Read().Query(
		`SELECT id, sample_rate FROM presets WHERE format = 'mp3' ORDER BY sort_order`)
	if err != nil {
		t.Fatalf("query preset: %v", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id string
		var rate *int
		if err := rows.Scan(&id, &rate); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if rate == nil || *rate != 48000 {
			t.Errorf("preset %s: sample_rate = %v, mau 48000", id, rate)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterasi: %v", err)
	}

	var quality *int
	if err := d.Read().QueryRow(
		`SELECT vbr_quality FROM presets WHERE id = 'mp3_vbr_v0'`).Scan(&quality); err != nil {
		t.Fatalf("baca vbr_quality: %v", err)
	}
	if quality == nil || *quality != 0 {
		t.Errorf("vbr_quality = %v, mau 0", quality)
	}
}

// Setiap format video menawarkan lima kualitas yang sama dengan batas
// resolusi menaik dan Terbaik tanpa batas; semuanya boleh menyalin stream
// sumber dan audionya mengikuti sample rate sumber.
func TestSeedPresetVideo(t *testing.T) {
	d := migrated(t)
	list, err := db.NewPresetRepository(d).List(context.Background(), false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	heights := []int{360, 480, 720, 1080, 0}
	byFormat := map[string][]domain.Preset{}
	var formats []string
	for _, p := range list {
		if !p.IsVideo() {
			if p.MaxHeight != nil {
				t.Errorf("preset audio %s punya max_height %d", p.ID, *p.MaxHeight)
			}
			continue
		}
		if len(byFormat[p.Format]) == 0 {
			formats = append(formats, p.Format)
		}
		byFormat[p.Format] = append(byFormat[p.Format], p)
	}
	if strings.Join(formats, ",") != "mp4,mkv,mov,webm,avi,flv" {
		t.Errorf("urutan format video = %v", formats)
	}

	for format, ps := range byFormat {
		if _, ok := domain.FormatOf(format); !ok {
			t.Errorf("format %s tidak punya profil di domain", format)
		}
		if len(ps) != len(heights) {
			t.Errorf("%s: %d preset, mau %d", format, len(ps), len(heights))
			continue
		}
		for i, p := range ps {
			got := 0
			if p.MaxHeight != nil {
				got = *p.MaxHeight
			}
			if got != heights[i] || !p.Passthrough || p.SampleRate != nil {
				t.Errorf("preset %s: max_height=%d passthrough=%v sample_rate=%v", p.ID, got, p.Passthrough, p.SampleRate)
			}
			wantID := fmt.Sprintf("%s_%d", format, got)
			if got == 0 {
				wantID = format + "_best"
			}
			if p.ID != wantID {
				t.Errorf("id %s, mau %s", p.ID, wantID)
			}
		}
	}
}

// Preset audio baru: lossless tanpa bitrate dan mengikuti sample rate
// sumber, dan hanya Opus asli yang menyalin stream sumber.
func TestSeedPresetAudio(t *testing.T) {
	d := migrated(t)
	repo := db.NewPresetRepository(d)
	ctx := context.Background()

	for _, id := range []string{"flac", "wav_pcm16", "m4a_alac"} {
		p, err := repo.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get(%s) error = %v", id, err)
		}
		if p.Mode != domain.ModeLossless || p.BitrateKbps != nil || p.VBRQuality != nil || p.SampleRate != nil {
			t.Errorf("preset %s = %+v, mau lossless tanpa bitrate dan ikut sumber", id, p)
		}
	}

	list, err := repo.List(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if p.IsVideo() {
			continue
		}
		if _, ok := domain.FormatOf(p.Format); !ok {
			t.Errorf("format %s tidak punya profil di domain", p.Format)
		}
		if p.Passthrough != (p.ID == "opus_source") {
			t.Errorf("preset %s: passthrough = %v", p.ID, p.Passthrough)
		}
	}

	// CHECK baru tetap menolak preset lossy tanpa bitrate.
	if _, err := d.Write().ExecContext(ctx, `INSERT INTO presets
		(id, label, format, codec, mode, channels, sort_order) VALUES ('x', 'x', 'mp3', 'libmp3lame', 'cbr', 2, 999)`); err == nil {
		t.Error("preset cbr tanpa bitrate seharusnya ditolak")
	}
}

// Index unik parsial menegakkan DUPLICATE_ACTIVE_JOB di level database,
// sehingga race antar goroutine tidak bisa menyelipkan job kembar.
func TestIndexDedupJobAktif(t *testing.T) {
	d := migrated(t)
	insert := `INSERT INTO jobs (id, source_url, source_key, status, preset_id, created_at)
	           VALUES (?, 'https://x.test', 'youtube:abc', ?, 'mp3_standard', '2026-01-01T00:00:00Z')`

	if _, err := d.Write().Exec(insert, "job_1", "queued"); err != nil {
		t.Fatalf("insert pertama: %v", err)
	}

	if _, err := d.Write().Exec(insert, "job_2", "queued"); err == nil {
		t.Error("job aktif kedua dengan source_key dan preset sama seharusnya ditolak")
	}

	// Setelah job pertama terminal, job baru untuk sumber yang sama boleh
	// dibuat lagi.
	if _, err := d.Write().Exec(
		`UPDATE jobs SET status = 'completed' WHERE id = 'job_1'`); err != nil {
		t.Fatalf("selesaikan job pertama: %v", err)
	}
	if _, err := d.Write().Exec(insert, "job_3", "queued"); err != nil {
		t.Errorf("job baru setelah yang lama terminal seharusnya boleh: %v", err)
	}
}

// CHECK pada job_events menolak tipe progress, menegakkan ADR-023 di level
// skema supaya tidak bisa dilanggar tanpa sengaja dari kode.
func TestJobEventsMenolakProgress(t *testing.T) {
	d := migrated(t)

	if _, err := d.Write().Exec(
		`INSERT INTO jobs (id, source_url, source_key, status, preset_id, created_at)
		 VALUES ('job_1', 'https://x.test', 'youtube:abc', 'queued', 'mp3_standard', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert job: %v", err)
	}

	_, err := d.Write().Exec(
		`INSERT INTO job_events (job_id, seq, type, payload, created_at)
		 VALUES ('job_1', 1, 'progress', '{}', '2026-01-01T00:00:00Z')`)
	if err == nil {
		t.Error("event bertipe progress seharusnya ditolak skema")
	}

	if _, err := d.Write().Exec(
		`INSERT INTO job_events (job_id, seq, type, payload, created_at)
		 VALUES ('job_1', 1, 'state', '{}', '2026-01-01T00:00:00Z')`); err != nil {
		t.Errorf("event state seharusnya diterima: %v", err)
	}
}

// Menghapus job harus ikut menghapus baris turunannya.
func TestForeignKeyCascade(t *testing.T) {
	d := migrated(t)

	if _, err := d.Write().Exec(
		`INSERT INTO jobs (id, source_url, source_key, status, preset_id, created_at)
		 VALUES ('job_1', 'https://x.test', 'youtube:abc', 'completed', 'mp3_standard', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := d.Write().Exec(
		`INSERT INTO files (id, job_id, path, filename, mime, size_bytes, sha256, created_at)
		 VALUES ('file_1', 'job_1', 'C:/x.mp3', 'x.mp3', 'audio/mpeg', 100, 'abc', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert file: %v", err)
	}

	if _, err := d.Write().Exec(`DELETE FROM jobs WHERE id = 'job_1'`); err != nil {
		t.Fatalf("hapus job: %v", err)
	}

	var n int
	if err := d.Read().QueryRow(`SELECT count(*) FROM files WHERE job_id = 'job_1'`).
		Scan(&n); err != nil {
		t.Fatalf("hitung file: %v", err)
	}
	if n != 0 {
		t.Errorf("file tersisa %d, mau 0 (foreign_keys mungkin tidak aktif)", n)
	}
}

// Transisi status dan penulisan job_events wajib berbagi satu transaksi.
// Kalau rollback tidak bekerja, keduanya bisa terpisah dan history jadi
// tidak konsisten dengan status job.
func TestInTxRollbackSaatError(t *testing.T) {
	d := migrated(t)
	ctx := context.Background()
	sentinel := errors.New("gagal di tengah")

	err := d.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO jobs (id, source_url, source_key, status, preset_id, created_at)
			 VALUES ('job_1', 'https://x.test', 'youtube:abc', 'queued', 'mp3_standard', '2026-01-01T00:00:00Z')`,
		); err != nil {
			return err
		}
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("InTx() error = %v, mau %v", err, sentinel)
	}

	var n int
	if err := d.Read().QueryRow(`SELECT count(*) FROM jobs`).Scan(&n); err != nil {
		t.Fatalf("hitung job: %v", err)
	}
	if n != 0 {
		t.Errorf("ada %d job tersisa, mau 0 (rollback gagal)", n)
	}
}

func TestInTxCommitSaatSukses(t *testing.T) {
	d := migrated(t)
	ctx := context.Background()

	err := d.InTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO jobs (id, source_url, source_key, status, preset_id, created_at)
			 VALUES ('job_1', 'https://x.test', 'youtube:abc', 'queued', 'mp3_standard', '2026-01-01T00:00:00Z')`)
		return err
	})
	if err != nil {
		t.Fatalf("InTx() error = %v", err)
	}

	var n int
	if err := d.Read().QueryRow(`SELECT count(*) FROM jobs`).Scan(&n); err != nil {
		t.Fatalf("hitung job: %v", err)
	}
	if n != 1 {
		t.Errorf("ada %d job, mau 1", n)
	}
}

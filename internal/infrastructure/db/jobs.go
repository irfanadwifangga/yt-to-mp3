package db

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// JobRepository menyimpan dan membaca job.
type JobRepository struct {
	db  *DB
	now func() time.Time
}

// NewJobRepository membuat repository job.
func NewJobRepository(d *DB) *JobRepository {
	return &JobRepository{db: d, now: func() time.Time { return time.Now().UTC() }}
}

// jobColumns dipakai bersama oleh seluruh query baca agar scanJob konsisten.
const jobColumns = `id, source_url, source_key, COALESCE(title, ''), status, preset_id,
	filename_mode, progress, COALESCE(phase, ''), attempt_count,
	COALESCE(error_code, ''), COALESCE(error_message, ''),
	created_at, started_at, finished_at`

// isUniqueViolation melaporkan apakah error berasal dari pelanggaran unik.
//
// Deteksi memakai kode SQLite, bukan pencocokan teks, supaya tidak ikut
// rusak ketika pesan error berubah antar versi driver.
func isUniqueViolation(err error) bool {
	var serr *sqlite.Error
	if errors.As(err, &serr) {
		code := serr.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
			code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// Create menyimpan job baru.
func (r *JobRepository) Create(ctx context.Context, j *domain.Job) error {
	if !j.Status.Valid() {
		return domain.NewError(domain.CodeInternal, domain.ClassLocal,
			fmt.Sprintf("status %q tidak dikenal", j.Status))
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = r.now()
	}

	_, err := r.db.Write().ExecContext(ctx, `
		INSERT INTO jobs (id, source_url, source_key, title, status, preset_id,
		                  filename_mode, progress, phase, attempt_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID, j.SourceURL, j.SourceKey, nullString(j.Title), string(j.Status), j.PresetID,
		string(j.FilenameMode), j.Progress, nullString(j.Phase), j.AttemptCount,
		formatTime(j.CreatedAt))

	if isUniqueViolation(err) {
		// Ditegakkan index unik parsial, bukan pengecekan di kode, sehingga
		// dua goroutine tidak bisa menyelipkan job kembar lewat celah race.
		return domain.NewError(domain.CodeDuplicateActive, domain.ClassPermanent,
			fmt.Sprintf("job aktif untuk %s dengan preset %s sudah ada", j.SourceKey, j.PresetID))
	}
	if err != nil {
		return fmt.Errorf("simpan job: %w", err)
	}
	return nil
}

// Get mengambil satu job.
func (r *JobRepository) Get(ctx context.Context, id string) (*domain.Job, error) {
	row := r.db.Read().QueryRowContext(ctx,
		`SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id)

	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent,
			fmt.Sprintf("job %s tidak ada", id))
	}
	if err != nil {
		return nil, fmt.Errorf("baca job: %w", err)
	}
	return j, nil
}

const (
	defaultLimit = 25
	maxLimit     = 100
)

// List mengembalikan satu halaman history beserta cursor halaman berikutnya.
//
// Pagination memakai keyset, bukan OFFSET: history bertambah dari ujung
// terbaru, dan OFFSET akan melewatkan atau menggandakan baris ketika ada
// job baru di tengah penelusuran.
func (r *JobRepository) List(ctx context.Context, q application.JobListQuery) ([]*domain.Job, string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	var (
		where []string
		args  []any
	)
	if q.Status != "" {
		where = append(where, "status = ?")
		args = append(args, string(q.Status))
	}
	if q.Cursor != "" {
		createdAt, id, err := decodeCursor(q.Cursor)
		if err != nil {
			return nil, "", domain.WrapError(domain.CodeInternal, domain.ClassLocal,
				"cursor tidak valid", err)
		}
		where = append(where, "(created_at, id) < (?, ?)")
		args = append(args, createdAt, id)
	}

	query := `SELECT ` + jobColumns + ` FROM jobs`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	// Ambil satu baris ekstra untuk mengetahui adanya halaman berikutnya.
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, limit+1)

	rows, err := r.db.Read().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	jobs := make([]*domain.Job, 0, limit)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, "", fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterasi history: %w", err)
	}

	var next string
	if len(jobs) > limit {
		last := jobs[limit-1]
		jobs = jobs[:limit]
		next = encodeCursor(formatTime(last.CreatedAt), last.ID)
	}
	return jobs, next, nil
}

func encodeCursor(createdAt, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt + "\x00" + id))
}

func decodeCursor(cursor string) (string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", "", err
	}
	createdAt, id, ok := strings.Cut(string(raw), "\x00")
	if !ok {
		return "", "", errors.New("bentuk cursor tidak dikenal")
	}
	return createdAt, id, nil
}

// Transition memindahkan job ke status baru dan mencatat event dalam satu
// transaksi.
//
// Ini satu-satunya jalan menulis kolom status. Validasi memakai
// domain.CanTransition sehingga aturan state machine tidak terduplikasi di
// lapisan penyimpanan.
func (r *JobRepository) Transition(
	ctx context.Context, id string, from, to domain.JobStatus, ev domain.Event,
) error {
	if !domain.CanTransition(from, to) {
		return domain.ErrInvalidTransition(from, to)
	}
	if ev.Type != "" && !ev.Type.Valid() {
		return domain.NewError(domain.CodeInternal, domain.ClassLocal,
			fmt.Sprintf("event bertipe %q tidak boleh dipersist", ev.Type))
	}

	now := r.now()
	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		var current string
		err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, id).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent,
				fmt.Sprintf("job %s tidak ada", id))
		}
		if err != nil {
			return fmt.Errorf("baca status: %w", err)
		}

		// Status di database harus masih sama dengan yang diyakini pemanggil,
		// kalau tidak berarti ada penulis lain yang mendahului.
		if domain.JobStatus(current) != from {
			return domain.NewError(domain.CodeInternal, domain.ClassLocal,
				fmt.Sprintf("job %s berstatus %s, bukan %s", id, current, from))
		}

		if err := updateStatus(ctx, tx, id, to, now); err != nil {
			return err
		}
		if ev.Type == "" {
			return nil
		}
		return insertEvent(ctx, tx, id, ev, now)
	})
}

// updateStatus menulis status baru beserta timestamp yang menyertainya.
func updateStatus(
	ctx context.Context, tx *sql.Tx, id string, to domain.JobStatus, now time.Time,
) error {
	query := `UPDATE jobs SET status = ?`
	args := []any{string(to)}

	// started_at diisi sekali saat job pertama kali meninggalkan antrean.
	if to == domain.StatusResolving {
		query += `, started_at = COALESCE(started_at, ?)`
		args = append(args, formatTime(now))
	}
	if to.IsTerminal() {
		query += `, finished_at = ?`
		args = append(args, formatTime(now))
	}
	query += ` WHERE id = ?`
	args = append(args, id)

	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("tulis status: %w", err)
	}
	return nil
}

// insertEvent menambahkan satu event dengan seq berikutnya.
func insertEvent(
	ctx context.Context, tx *sql.Tx, jobID string, ev domain.Event, now time.Time,
) error {
	var seq int64
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM job_events WHERE job_id = ?`, jobID).Scan(&seq)
	if err != nil {
		return fmt.Errorf("hitung seq: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO job_events (job_id, seq, type, payload, created_at) VALUES (?, ?, ?, ?, ?)`,
		jobID, seq, string(ev.Type), ev.Payload, formatTime(now))
	if err != nil {
		return fmt.Errorf("tulis event: %w", err)
	}
	return nil
}

// Fail memindahkan job ke failed beserta kode errornya.
func (r *JobRepository) Fail(
	ctx context.Context, id string, from domain.JobStatus, code domain.ErrorCode, detail string,
) error {
	now := r.now()
	if !domain.CanTransition(from, domain.StatusFailed) {
		return domain.ErrInvalidTransition(from, domain.StatusFailed)
	}

	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`UPDATE jobs SET status = 'failed', error_code = ?, error_message = ?, finished_at = ?
			 WHERE id = ? AND status = ?`,
			string(code), detail, formatTime(now), id, string(from)); err != nil {
			return fmt.Errorf("tandai gagal: %w", err)
		}
		return insertEvent(ctx, tx, id,
			domain.Event{Type: domain.EventError, Payload: application.StreamEvent{
				Type: application.StreamError, Status: domain.StatusFailed, Code: code,
			}.PayloadJSON()}, now)
	})
}

// ClaimNextQueued memindahkan satu job antre ke resolving secara atomik.
//
// UPDATE ... RETURNING membuat pengambilan job aman walau kelak ada lebih
// dari satu scheduler: dua pemanggil tidak mungkin memenangkan baris sama.
func (r *JobRepository) ClaimNextQueued(ctx context.Context) (*domain.Job, error) {
	now := r.now()
	var claimed *domain.Job

	err := r.db.InTx(ctx, func(tx *sql.Tx) error {
		row := tx.QueryRowContext(ctx, `
			UPDATE jobs SET status = 'resolving', started_at = COALESCE(started_at, ?)
			WHERE id = (
				SELECT id FROM jobs WHERE status = 'queued'
				ORDER BY created_at ASC, id ASC LIMIT 1
			)
			RETURNING `+jobColumns, formatTime(now))

		j, err := scanJob(row)
		if errors.Is(err, sql.ErrNoRows) {
			return nil // antrean kosong, bukan kondisi error
		}
		if err != nil {
			return fmt.Errorf("claim job: %w", err)
		}
		claimed = j
		return insertEvent(ctx, tx, j.ID,
			domain.Event{Type: domain.EventState, Payload: application.StreamEvent{
				Type: application.StreamState, Status: domain.StatusResolving,
			}.PayloadJSON()}, now)
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// SweepNonTerminal menandai job yang tertinggal dari proses sebelumnya.
//
// Job berstatus aktif saat startup berarti proses OS pemiliknya sudah tidak
// ada, jadi tidak mungkin dilanjutkan. Job queued sengaja dibiarkan supaya
// bisa dijadwalkan ulang. Lihat docs architecture "Crash recovery".
func (r *JobRepository) SweepNonTerminal(ctx context.Context) (int, error) {
	now := r.now()
	var affected int

	err := r.db.InTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE jobs
			SET status = 'failed', error_code = ?, finished_at = ?
			WHERE status IN ('resolving','downloading','converting','verifying','cancelling')`,
			string(domain.CodeInterrupted), formatTime(now))
		if err != nil {
			return fmt.Errorf("sweep job tertinggal: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("hitung baris tersapu: %w", err)
		}
		affected = int(n)
		return nil
	})
	return affected, err
}

// PruneEvents membuang event milik job terminal yang selesai lebih lama dari
// olderThan.
//
// Event hanya dipakai untuk melanjutkan stream SSE lewat Last-Event-ID, dan
// job yang sudah lama selesai tidak punya stream untuk dilanjutkan. Job dan
// berkasnya tetap ada; yang dibuang hanya jejak transisinya. Lihat
// data-model "Retensi".
func (r *JobRepository) PruneEvents(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := formatTime(r.now().Add(-olderThan))

	res, err := r.db.Write().ExecContext(ctx, `
		DELETE FROM job_events
		WHERE job_id IN (
			SELECT id FROM jobs
			WHERE status IN ('completed','failed','cancelled')
			  AND finished_at IS NOT NULL
			  AND finished_at < ?)`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("pangkas event: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("hitung event terpangkas: %w", err)
	}
	return int(n), nil
}

// Delete menghapus job beserta baris turunannya.
func (r *JobRepository) Delete(ctx context.Context, id string) error {
	res, err := r.db.Write().ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("hapus job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("hitung baris terhapus: %w", err)
	}
	if n == 0 {
		return domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent,
			fmt.Sprintf("job %s tidak ada", id))
	}
	return nil
}

// Events mengembalikan event terpersist sesudah seq tertentu, dipakai untuk
// resume SSE lewat Last-Event-ID.
func (r *JobRepository) Events(ctx context.Context, jobID string, afterSeq int64) ([]domain.Event, error) {
	rows, err := r.db.Read().QueryContext(ctx,
		`SELECT job_id, seq, type, payload, created_at FROM job_events
		 WHERE job_id = ? AND seq > ? ORDER BY seq ASC`, jobID, afterSeq)
	if err != nil {
		return nil, fmt.Errorf("query event: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var createdAt string
		var typ string
		if err := rows.Scan(&ev.JobID, &ev.Seq, &typ, &ev.Payload, &createdAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.Type = domain.EventType(typ)
		if ev.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, fmt.Errorf("parse waktu event: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// scanner disatukan supaya QueryRow dan Query memakai jalur scan yang sama.
type scanner interface {
	Scan(dest ...any) error
}

func scanJob(s scanner) (*domain.Job, error) {
	var (
		j          domain.Job
		status     string
		mode       string
		progress   sql.NullFloat64
		createdAt  string
		startedAt  sql.NullString
		finishedAt sql.NullString
		errorCode  string
	)

	err := s.Scan(&j.ID, &j.SourceURL, &j.SourceKey, &j.Title, &status, &j.PresetID,
		&mode, &progress, &j.Phase, &j.AttemptCount, &errorCode, &j.ErrorMessage,
		&createdAt, &startedAt, &finishedAt)
	if err != nil {
		return nil, err
	}

	j.Status = domain.JobStatus(status)
	j.FilenameMode = domain.FilenameMode(mode)
	j.ErrorCode = domain.ErrorCode(errorCode)

	// progress NULL berarti indeterminate, bukan nol.
	if progress.Valid {
		v := progress.Float64
		j.Progress = &v
	}

	if j.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	if j.StartedAt, err = parseNullTime(startedAt); err != nil {
		return nil, fmt.Errorf("parse started_at: %w", err)
	}
	if j.FinishedAt, err = parseNullTime(finishedAt); err != nil {
		return nil, fmt.Errorf("parse finished_at: %w", err)
	}
	return &j, nil
}

func parseNullTime(v sql.NullString) (*time.Time, error) {
	if !v.Valid || v.String == "" {
		return nil, nil
	}
	t, err := parseTime(v.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// nullString menyimpan string kosong sebagai NULL agar kolom opsional tidak
// tercampur antara "kosong" dan "belum diisi".
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// CountActive mengembalikan jumlah job yang sedang berjalan dan yang antre.
//
// Dihitung dari database, bukan dari state di memori, supaya angkanya tetap
// benar setelah restart dan tidak bergantung pada worker yang hidup.
func (r *JobRepository) CountActive(ctx context.Context) (int, int, error) {
	var active, queued int
	err := r.db.Read().QueryRowContext(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE status IN ('resolving','downloading','converting','verifying','cancelling')),
			COUNT(*) FILTER (WHERE status = 'queued')
		FROM jobs`).Scan(&active, &queued)
	if err != nil {
		return 0, 0, fmt.Errorf("hitung job aktif: %w", err)
	}
	return active, queued, nil
}

// Complete menutup job sukses beserta berkas hasilnya dalam satu transaksi.
//
// Keduanya wajib menyatu: menulis berkas lalu gagal menutup job menyisakan
// baris yatim, sedangkan menutup job lalu gagal menulis berkas menghasilkan
// job "selesai" tanpa berkas. Lihat data-model "Invarian".
func (r *JobRepository) Complete(
	ctx context.Context, jobID string, from domain.JobStatus, f *domain.File, ev domain.Event,
) error {
	if !domain.CanTransition(from, domain.StatusCompleted) {
		return domain.ErrInvalidTransition(from, domain.StatusCompleted)
	}

	now := r.now()
	if f.CreatedAt.IsZero() {
		f.CreatedAt = now
	}

	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		var current string
		err := tx.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, jobID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent,
				fmt.Sprintf("job %s tidak ada", jobID))
		}
		if err != nil {
			return fmt.Errorf("baca status: %w", err)
		}
		if domain.JobStatus(current) != from {
			return domain.NewError(domain.CodeInternal, domain.ClassLocal,
				fmt.Sprintf("job %s berstatus %s, bukan %s", jobID, current, from))
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO files (id, job_id, path, filename, mime, size_bytes, sha256, missing, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
			f.ID, f.JobID, f.Path, f.Filename, f.MIME, f.SizeBytes, f.SHA256,
			formatTime(f.CreatedAt)); err != nil {
			return fmt.Errorf("simpan berkas: %w", err)
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE jobs SET status = 'completed', progress = 100, phase = NULL, finished_at = ?
			 WHERE id = ?`, formatTime(now), jobID); err != nil {
			return fmt.Errorf("tutup job: %w", err)
		}
		return insertEvent(ctx, tx, jobID, ev, now)
	})
}

// UpdateProgress menyimpan kemajuan pada batas fase.
//
// Hanya dipanggil saat fase berganti, bukan pada setiap pembaruan progress:
// menulis empat kali per detik per job akan membuat database jadi titik
// panas tanpa manfaat, sementara nilai live sudah mengalir lewat SSE.
func (r *JobRepository) UpdateProgress(
	ctx context.Context, jobID string, percent *float64, phase string,
) error {
	_, err := r.db.Write().ExecContext(ctx,
		`UPDATE jobs SET progress = ?, phase = ? WHERE id = ?`,
		percent, nullString(phase), jobID)
	if err != nil {
		return fmt.Errorf("simpan progress: %w", err)
	}
	return nil
}

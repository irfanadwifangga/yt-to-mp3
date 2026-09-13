package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// FileRepository menyimpan berkas hasil.
type FileRepository struct {
	db  *DB
	now func() time.Time
}

// NewFileRepository membuat repository berkas.
func NewFileRepository(d *DB) *FileRepository {
	return &FileRepository{db: d, now: func() time.Time { return time.Now().UTC() }}
}

const fileColumns = `id, job_id, path, filename, mime, size_bytes, sha256, missing, created_at`

// Create menyimpan berkas hasil sebuah job.
func (r *FileRepository) Create(ctx context.Context, f *domain.File) error {
	if f.CreatedAt.IsZero() {
		f.CreatedAt = r.now()
	}
	_, err := r.db.Write().ExecContext(ctx, `
		INSERT INTO files (id, job_id, path, filename, mime, size_bytes, sha256, missing, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		f.ID, f.JobID, f.Path, f.Filename, f.MIME, f.SizeBytes, f.SHA256, formatTime(f.CreatedAt))
	if err != nil {
		return fmt.Errorf("simpan berkas: %w", err)
	}
	return nil
}

// GetByJob mengambil berkas hasil sebuah job.
func (r *FileRepository) GetByJob(ctx context.Context, jobID string) (*domain.File, error) {
	row := r.db.Read().QueryRowContext(ctx,
		`SELECT `+fileColumns+` FROM files WHERE job_id = ?`, jobID)

	f, err := scanFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent,
			fmt.Sprintf("berkas untuk job %s tidak ada", jobID))
	}
	if err != nil {
		return nil, fmt.Errorf("baca berkas: %w", err)
	}
	return f, nil
}

// Get mengambil berkas berdasarkan id.
func (r *FileRepository) Get(ctx context.Context, id string) (*domain.File, error) {
	row := r.db.Read().QueryRowContext(ctx,
		`SELECT `+fileColumns+` FROM files WHERE id = ?`, id)

	f, err := scanFile(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent,
			fmt.Sprintf("berkas %s tidak ada", id))
	}
	if err != nil {
		return nil, fmt.Errorf("baca berkas: %w", err)
	}
	return f, nil
}

// MarkMissing menandai berkas yang sudah tidak ada di disk.
//
// Barisnya sengaja dipertahankan: history tetap menunjukkan bahwa konversi
// pernah berhasil, hanya berkasnya yang hilang. Lihat data-model "Retensi".
func (r *FileRepository) MarkMissing(ctx context.Context, id string) error {
	return r.SetMissing(ctx, id, true)
}

// SetMissing menyetel penanda keberadaan berkas.
//
// Penanda bisa dibalik: berkas di drive eksternal yang dicabut lalu dicolok
// kembali harus tersedia lagi di riwayat.
func (r *FileRepository) SetMissing(ctx context.Context, id string, missing bool) error {
	_, err := r.db.Write().ExecContext(ctx,
		`UPDATE files SET missing = ? WHERE id = ?`, boolToInt(missing), id)
	if err != nil {
		return fmt.Errorf("setel penanda berkas hilang: %w", err)
	}
	return nil
}

// All mengembalikan seluruh berkas, dipakai rekonsiliasi saat startup.
func (r *FileRepository) All(ctx context.Context) ([]*domain.File, error) {
	rows, err := r.db.Read().QueryContext(ctx, `SELECT `+fileColumns+` FROM files`)
	if err != nil {
		return nil, fmt.Errorf("query berkas: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan berkas: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func scanFile(s scanner) (*domain.File, error) {
	var (
		f         domain.File
		missing   int
		createdAt string
	)
	err := s.Scan(&f.ID, &f.JobID, &f.Path, &f.Filename, &f.MIME,
		&f.SizeBytes, &f.SHA256, &missing, &createdAt)
	if err != nil {
		return nil, err
	}
	f.Missing = missing != 0
	if f.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	return &f, nil
}

// RefsByJobs memetakan job ke id dan nama berkas hasilnya.
//
// Diambil sekali untuk seluruh halaman history, bukan satu query per baris:
// pola N+1 pada daftar 100 job menghasilkan 100 perjalanan ke database untuk
// data yang muat dalam satu.
func (r *FileRepository) RefsByJobs(ctx context.Context, jobIDs []string) (map[string]application.FileRef, error) {
	out := make(map[string]application.FileRef, len(jobIDs))
	if len(jobIDs) == 0 {
		return out, nil
	}

	placeholders := strings.Repeat("?,", len(jobIDs)-1) + "?"
	args := make([]any, len(jobIDs))
	for i, id := range jobIDs {
		args[i] = id
	}

	rows, err := r.db.Read().QueryContext(ctx,
		`SELECT job_id, id, filename FROM files WHERE missing = 0 AND job_id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query id berkas: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var jobID string
		var ref application.FileRef
		if err := rows.Scan(&jobID, &ref.ID, &ref.Filename); err != nil {
			return nil, fmt.Errorf("scan berkas: %w", err)
		}
		out[jobID] = ref
	}
	return out, rows.Err()
}

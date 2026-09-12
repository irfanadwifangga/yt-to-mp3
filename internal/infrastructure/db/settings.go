package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SettingsRepository menyimpan override konfigurasi dari pengguna.
//
// Hanya nilai yang benar-benar diubah yang tersimpan; kunci yang tidak ada
// berarti memakai default bawaan. Dengan begitu perubahan default di versi
// baru tetap sampai ke pengguna yang tidak pernah menyentuh setelan itu.
type SettingsRepository struct {
	db  *DB
	now func() time.Time
}

// NewSettingsRepository membuat repository setelan.
func NewSettingsRepository(d *DB) *SettingsRepository {
	return &SettingsRepository{db: d, now: func() time.Time { return time.Now().UTC() }}
}

// All mengembalikan seluruh override yang tersimpan.
func (r *SettingsRepository) All(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.Read().QueryContext(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, fmt.Errorf("baca setelan: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan setelan: %w", err)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Put menyimpan sekumpulan override dalam satu transaksi, sehingga
// penyimpanan tidak pernah berakhir setengah jadi.
func (r *SettingsRepository) Put(ctx context.Context, values map[string]string) error {
	now := formatTime(r.now())

	return r.db.InTx(ctx, func(tx *sql.Tx) error {
		for key, value := range values {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO settings (key, value, type, updated_at)
				VALUES (?, ?, 'string', ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
				key, value, now); err != nil {
				return fmt.Errorf("simpan setelan %s: %w", key, err)
			}
		}
		return nil
	})
}

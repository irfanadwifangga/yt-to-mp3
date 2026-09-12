package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/irfanadwifangga/yt-to-mp3/migrations"
)

// Migrate menjalankan seluruh migrasi yang belum diterapkan.
//
// Migrasi bersifat maju saja saat runtime. Penurunan versi ditangani oleh
// penjagaan schema_version, bukan rollback otomatis.
func (d *DB) Migrate(ctx context.Context, appVersion string) error {
	if err := d.guardSchemaVersion(ctx); err != nil {
		return err
	}

	provider, err := goose.NewProvider(goose.DialectSQLite3, d.write, migrations.FS)
	if err != nil {
		return fmt.Errorf("siapkan migrator: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("jalankan migrasi: %w", err)
	}
	for _, r := range results {
		d.log.Info("migrasi diterapkan", "versi", r.Source.Version, "berkas", r.Source.Path)
	}

	return d.stampAppVersion(ctx, appVersion)
}

// guardSchemaVersion menolak database yang lebih baru daripada binary ini.
//
// Tanpa penjagaan ini, menjalankan versi lama di atas database yang sudah
// dimigrasi versi baru akan gagal di tengah jalan dengan pesan yang
// menyesatkan, bukan menolak sejak awal.
func (d *DB) guardSchemaVersion(ctx context.Context) error {
	var found int
	err := d.read.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_meta'`,
	).Scan(&found)
	if err != nil {
		return fmt.Errorf("periksa schema_meta: %w", err)
	}
	if found == 0 {
		return nil // database baru, belum ada apa-apa untuk dijaga
	}

	var version int
	err = d.read.QueryRowContext(ctx, `SELECT schema_version FROM schema_meta WHERE id = 1`).
		Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("baca schema_version: %w", err)
	}

	if version > SchemaVersion {
		return fmt.Errorf(
			"database memakai skema versi %d sedangkan aplikasi ini hanya mengenal %d; "+
				"jalankan versi aplikasi yang lebih baru", version, SchemaVersion)
	}
	return nil
}

// stampAppVersion mencatat versi aplikasi yang terakhir membuka database,
// supaya laporan bug dapat dikaitkan ke binary yang benar.
func (d *DB) stampAppVersion(ctx context.Context, appVersion string) error {
	_, err := d.write.ExecContext(ctx,
		`UPDATE schema_meta SET app_version = ?, schema_version = ? WHERE id = 1`,
		appVersion, SchemaVersion)
	if err != nil {
		return fmt.Errorf("catat versi aplikasi: %w", err)
	}
	return nil
}

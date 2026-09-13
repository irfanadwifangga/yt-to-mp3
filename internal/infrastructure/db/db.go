// Package db menyediakan koneksi SQLite, repository, dan runner migrasi.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // driver pure Go, prasyarat cross-compile (ADR-011)
)

// SchemaVersion adalah versi skema yang dikenali binary ini.
//
// Database dengan versi lebih tinggi menolak dibuka: itu berarti aplikasi
// lebih lama daripada datanya, dan menjalankannya akan merusak dengan cara
// yang membingungkan.
const SchemaVersion = 2

// pragmas berlaku per koneksi. journal_mode bersifat persisten di berkas,
// sisanya harus diset ulang setiap koneksi baru, sehingga dipasang lewat DSN.
var pragmas = []string{
	"busy_timeout(5000)",
	"foreign_keys(ON)",
	"journal_mode(WAL)",
	"synchronous(NORMAL)",
}

// DB memisahkan jalur baca dan tulis.
//
// WAL mengizinkan banyak pembaca berbarengan dengan satu penulis. Membatasi
// seluruh pool ke satu koneksi akan menyerialkan pembacaan tanpa perlu,
// sehingga query history dan health ikut antre di belakang penulisan job.
// Lihat ADR-024.
type DB struct {
	read  *sql.DB
	write *sql.DB
	path  string
	log   *slog.Logger
}

// dsnFor menyusun DSN SQLite untuk sebuah path.
//
// Dua jebakan Windows sekaligus ditangani di sini. Pertama, path harus
// berbentuk "file:///C:/..." dengan tiga garis miring; "file:C:/..." dibaca
// SQLite sebagai path relatif lalu gagal dengan SQLITE_CANTOPEN. Kedua,
// direktori data Windows lazim memuat spasi (nama pengguna), dan spasi
// mentah merusak parsing URI, jadi path di-escape.
func dsnFor(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // "C:/x" -> "/C:/x" sehingga menghasilkan file:///C:/x
	}
	u := url.URL{Scheme: "file", Path: p}
	return fmt.Sprintf("%s?_pragma=%s", u.String(), strings.Join(pragmas, "&_pragma="))
}

// Open membuka database dan menyiapkan kedua pool.
func Open(ctx context.Context, path string, log *slog.Logger) (*DB, error) {
	dsn := dsnFor(path)

	write, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("buka database untuk tulis: %w", err)
	}
	// Satu penulis saja: SQLite hanya mengizinkan satu transaksi tulis pada
	// satu waktu, dan membatasinya di sini mencegah SQLITE_BUSY.
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)
	write.SetConnMaxLifetime(0)

	read, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("buka database untuk baca: %w", err)
	}
	read.SetMaxOpenConns(4)
	read.SetMaxIdleConns(4)
	read.SetConnMaxLifetime(time.Hour)

	d := &DB{read: read, write: write, path: path, log: log}

	if err := d.ping(ctx); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := d.write.PingContext(ctx); err != nil {
		return fmt.Errorf("ping jalur tulis: %w", err)
	}
	if err := d.read.PingContext(ctx); err != nil {
		return fmt.Errorf("ping jalur baca: %w", err)
	}
	return nil
}

// Read mengembalikan pool baca.
func (d *DB) Read() *sql.DB { return d.read }

// Write mengembalikan koneksi tulis tunggal.
func (d *DB) Write() *sql.DB { return d.write }

// Path mengembalikan lokasi berkas database.
func (d *DB) Path() string { return d.path }

// Close menutup kedua pool.
func (d *DB) Close() error {
	return errors.Join(d.read.Close(), d.write.Close())
}

// InTx menjalankan fn dalam satu transaksi tulis dan menggulungnya bila
// terjadi error atau panic.
//
// Transisi status job dan penulisan job_events wajib berbagi transaksi yang
// sama; ini pintu tunggalnya.
func (d *DB) InTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mulai transaksi: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	return tx.Commit()
}

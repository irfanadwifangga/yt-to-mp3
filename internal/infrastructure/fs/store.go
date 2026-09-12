package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// maxCollisionAttempts membatasi penomoran sufiks sebelum menyerah.
const maxCollisionAttempts = 999

// Store mengelola direktori keluaran dan berkas sementara.
type Store struct {
	outputDir string
	dataTmp   string
}

// NewStore membuat store dan menyiapkan direktorinya.
//
// Temp untuk commit akhir berada di dalam direktori keluaran, bukan di
// direktori data: os.Rename hanya atomik dalam satu volume, dan pengguna
// bebas memindahkan folder keluaran ke drive lain.
func NewStore(outputDir, dataTmp string) (*Store, error) {
	s := &Store{outputDir: outputDir, dataTmp: dataTmp}
	for _, dir := range []string{outputDir, s.CommitTemp(), dataTmp} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("buat direktori %s: %w", dir, err)
		}
	}
	return s, nil
}

// OutputDir mengembalikan direktori keluaran.
func (s *Store) OutputDir() string { return s.outputDir }

// CommitTemp mengembalikan direktori temp yang sevolume dengan keluaran.
func (s *Store) CommitTemp() string { return filepath.Join(s.outputDir, ".tmp") }

// Reservation adalah nama berkas yang sudah dimenangkan.
type Reservation struct {
	Path     string
	Filename string

	released bool
}

// Reserve memenangkan sebuah nama berkas secara atomik.
//
// Penciptaan memakai O_EXCL, bukan pengecekan "apakah berkas ada" lebih
// dulu. Pemeriksaan naif menyisakan celah antara cek dan tulis, sehingga dua
// worker bisa sama-sama mengira nama itu bebas lalu saling menimpa. Lihat
// ADR-029.
func (s *Store) Reserve(filename string) (*Reservation, error) {
	for attempt := range maxCollisionAttempts {
		candidate := filename
		if attempt > 0 {
			candidate = WithSuffix(filename, attempt+1)
		}
		path := filepath.Join(s.outputDir, candidate)

		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		switch {
		case err == nil:
			_ = f.Close()
			return &Reservation{Path: path, Filename: candidate}, nil
		case errors.Is(err, os.ErrExist):
			continue // nama sudah dipakai, coba sufiks berikutnya
		default:
			return nil, fmt.Errorf("reservasi %s: %w", candidate, err)
		}
	}
	return nil, fmt.Errorf("tidak menemukan nama bebas untuk %s", filename)
}

// Release melepaskan reservasi yang belum jadi berkas sungguhan.
//
// Aman dipanggil setelah Commit: penanda sudah tergantikan oleh berkas asli,
// dan pemanggilan kedua tidak melakukan apa pun.
func (s *Store) Release(r *Reservation) {
	if r == nil || r.released {
		return
	}
	r.released = true

	// Hanya hapus bila masih berupa penanda kosong; berkas hasil commit
	// tidak boleh ikut terhapus.
	if info, err := os.Stat(r.Path); err == nil && info.Size() == 0 {
		_ = os.Remove(r.Path)
	}
}

// Commit memindahkan berkas sementara ke tempat final.
//
// Rename dipakai karena ia atomik dalam satu volume: pengamat tidak pernah
// melihat berkas setengah tertulis, hanya belum ada atau sudah lengkap.
func (s *Store) Commit(tmpPath string, r *Reservation) error {
	if r == nil {
		return errors.New("reservasi kosong")
	}
	if err := os.Rename(tmpPath, r.Path); err != nil {
		return fmt.Errorf("pindahkan hasil ke %s: %w", r.Filename, err)
	}
	r.released = true // penanda sudah tergantikan berkas asli
	return nil
}

// TempDirFor membuat direktori kerja untuk satu job.
func (s *Store) TempDirFor(jobID string) (string, error) {
	dir := filepath.Join(s.dataTmp, jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("buat temp job: %w", err)
	}
	return dir, nil
}

// CommitTempFile membuat berkas sementara sevolume dengan keluaran.
func (s *Store) CommitTempFile(jobID, ext string) (string, error) {
	f, err := os.CreateTemp(s.CommitTemp(), jobID+"-*"+ext)
	if err != nil {
		return "", fmt.Errorf("buat temp commit: %w", err)
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("tutup temp commit: %w", err)
	}
	// FFmpeg menulis sendiri berkasnya; berkas kosong ini hanya memesan nama.
	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("siapkan temp commit: %w", err)
	}
	return path, nil
}

// RemoveTempDir membersihkan direktori kerja sebuah job.
func (s *Store) RemoveTempDir(jobID string) error {
	err := os.RemoveAll(filepath.Join(s.dataTmp, jobID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("bersihkan temp job: %w", err)
	}
	return nil
}

// GCTemp membuang sisa berkas sementara yang lebih tua dari maxAge.
//
// Dijalankan saat startup dan berkala: proses yang mati mendadak selalu
// meninggalkan temp, dan tanpa pembersihan direktori itu tumbuh selamanya.
func (s *Store) GCTemp(maxAge time.Duration, active map[string]bool) (int, error) {
	var removed int
	cutoff := time.Now().Add(-maxAge)

	for _, dir := range []string{s.dataTmp, s.CommitTemp()} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return removed, fmt.Errorf("baca %s: %w", dir, err)
		}

		for _, entry := range entries {
			if active[entry.Name()] {
				continue
			}
			info, err := entry.Info()
			if err != nil || info.ModTime().After(cutoff) {
				continue
			}
			if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// ReservePath adalah pembungkus Reserve yang hanya memakai tipe dasar,
// sehingga layer application dapat memakainya tanpa mengenal paket ini.
func (s *Store) ReservePath(filename string) (path, name string, release func(), err error) {
	r, err := s.Reserve(filename)
	if err != nil {
		return "", "", nil, err
	}
	return r.Path, r.Filename, func() { s.Release(r) }, nil
}

// CommitPath memindahkan berkas sementara ke tempat final.
func (s *Store) CommitPath(tmpPath, finalPath string) error {
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("pindahkan hasil ke %s: %w", filepath.Base(finalPath), err)
	}
	return nil
}

// FreeOutputSpace mengembalikan ruang kosong pada volume keluaran.
func (s *Store) FreeOutputSpace() (uint64, error) {
	return FreeSpace(s.outputDir)
}

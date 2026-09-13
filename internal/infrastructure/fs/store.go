package fs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
)

// maxCollisionAttempts membatasi penomoran sufiks sebelum menyerah.
const maxCollisionAttempts = 999

// Store mengelola direktori keluaran dan berkas sementara.
//
// Direktori keluaran boleh diganti selagi aplikasi berjalan. Karena itu job
// tidak membaca direktori langsung dari Store, melainkan mengambil satu
// snapshot Target di awal dan memakainya sampai selesai: bila direktori
// berganti di tengah konversi, reservasi nama dan berkas sementara job itu
// tetap berada di volume yang sama, sehingga rename akhir tetap atomik.
type Store struct {
	dataTmp string
	current atomic.Pointer[Target]
}

// NewStore membuat store dan menyiapkan direktorinya.
func NewStore(outputDir, dataTmp string) (*Store, error) {
	if err := os.MkdirAll(dataTmp, 0o755); err != nil {
		return nil, fmt.Errorf("buat direktori %s: %w", dataTmp, err)
	}
	s := &Store{dataTmp: dataTmp}
	if err := s.SetOutputDir(outputDir); err != nil {
		return nil, err
	}
	return s, nil
}

// SetOutputDir memvalidasi lalu memasang direktori keluaran baru.
//
// Direktori lama tetap dipakai sampai direktori baru terbukti bisa ditulisi,
// jadi kegagalan di sini tidak pernah meninggalkan store tanpa tujuan.
func (s *Store) SetOutputDir(dir string) error {
	target, err := prepareTarget(dir)
	if err != nil {
		return err
	}
	s.current.Store(target)
	return nil
}

func prepareTarget(dir string) (*Target, error) {
	if dir == "" {
		return nil, errors.New("direktori keluaran kosong")
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("direktori keluaran harus berupa path absolut: %s", dir)
	}

	t := &Target{dir: filepath.Clean(dir)}
	if err := os.MkdirAll(t.CommitTemp(), 0o755); err != nil {
		return nil, fmt.Errorf("buat direktori %s: %w", t.dir, err)
	}

	// Folder yang ada belum tentu bisa ditulisi, misalnya folder sistem atau
	// drive read-only. Diuji sekarang supaya kegagalannya muncul saat folder
	// dipilih, bukan di akhir konversi pertama.
	probe, err := os.CreateTemp(t.CommitTemp(), "probe-*")
	if err != nil {
		return nil, fmt.Errorf("direktori %s tidak dapat ditulisi: %w", t.dir, err)
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)

	return t, nil
}

// Output mengembalikan snapshot direktori keluaran saat ini.
func (s *Store) Output() application.OutputTarget { return s.current.Load() }

// OutputDir mengembalikan direktori keluaran saat ini.
func (s *Store) OutputDir() string { return s.current.Load().dir }

// CommitTemp mengembalikan direktori temp yang sevolume dengan keluaran.
func (s *Store) CommitTemp() string { return s.current.Load().CommitTemp() }

// Reserve memenangkan nama berkas pada direktori keluaran saat ini.
func (s *Store) Reserve(filename string) (*Reservation, error) {
	return s.current.Load().Reserve(filename)
}

// Release melepaskan reservasi yang belum jadi berkas sungguhan.
func (s *Store) Release(r *Reservation) { release(r) }

// Commit memindahkan berkas sementara ke tempat yang sudah direservasi.
func (s *Store) Commit(tmpPath string, r *Reservation) error { return commit(tmpPath, r) }

// TempDirFor membuat direktori kerja untuk satu job.
func (s *Store) TempDirFor(jobID string) (string, error) {
	dir := filepath.Join(s.dataTmp, jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("buat temp job: %w", err)
	}
	return dir, nil
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
// Dijalankan saat startup: proses yang mati mendadak selalu meninggalkan
// temp, dan tanpa pembersihan direktori itu tumbuh selamanya.
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
			if belongsToActive(entry.Name(), active) {
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

// belongsToActive melaporkan apakah entri temp milik job yang sedang
// berjalan.
//
// Direktori kerja bernama persis id job, sedangkan berkas commit sementara
// bernama "<id>-<acak>.<ext>" (lihat CommitTempFile). Pencocokan nama persis
// saja akan membuat berkas commit milik job aktif ikut terhapus.
func belongsToActive(name string, active map[string]bool) bool {
	if active[name] {
		return true
	}
	for id := range active {
		if strings.HasPrefix(name, id+"-") {
			return true
		}
	}
	return false
}

// Target adalah satu direktori keluaran yang dipakai sebuah job dari awal
// sampai commit.
type Target struct {
	dir string
}

// Dir mengembalikan path direktori keluaran.
func (t *Target) Dir() string { return t.dir }

// CommitTemp mengembalikan direktori temp yang sevolume dengan keluaran.
//
// Temp untuk commit akhir berada di dalam direktori keluaran, bukan di
// direktori data: os.Rename hanya atomik dalam satu volume, dan pengguna
// bebas memilih folder keluaran di drive lain.
func (t *Target) CommitTemp() string { return filepath.Join(t.dir, ".tmp") }

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
func (t *Target) Reserve(filename string) (*Reservation, error) {
	for attempt := range maxCollisionAttempts {
		candidate := filename
		if attempt > 0 {
			candidate = WithSuffix(filename, attempt+1)
		}
		path := filepath.Join(t.dir, candidate)

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

// ReservePath adalah pembungkus Reserve yang hanya memakai tipe dasar,
// sehingga layer application dapat memakainya tanpa mengenal paket ini.
func (t *Target) ReservePath(filename string) (path, name string, releaseFn func(), err error) {
	r, err := t.Reserve(filename)
	if err != nil {
		return "", "", nil, err
	}
	return r.Path, r.Filename, func() { release(r) }, nil
}

// CommitTempFile menyiapkan nama berkas sementara sevolume dengan keluaran.
func (t *Target) CommitTempFile(jobID, ext string) (string, error) {
	f, err := os.CreateTemp(t.CommitTemp(), jobID+"-*"+ext)
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

// CommitPath memindahkan berkas sementara ke tempat final.
//
// Rename dipakai karena ia atomik dalam satu volume: pengamat tidak pernah
// melihat berkas setengah tertulis, hanya belum ada atau sudah lengkap.
func (t *Target) CommitPath(tmpPath, finalPath string) error {
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("pindahkan hasil ke %s: %w", filepath.Base(finalPath), err)
	}
	return nil
}

// FreeSpace mengembalikan ruang kosong pada volume keluaran.
func (t *Target) FreeSpace() (uint64, error) {
	return FreeSpace(t.dir)
}

// release menghapus penanda reservasi yang belum tergantikan berkas asli.
//
// Aman dipanggil setelah commit dan aman dipanggil dua kali.
func release(r *Reservation) {
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

func commit(tmpPath string, r *Reservation) error {
	if r == nil {
		return errors.New("reservasi kosong")
	}
	if err := os.Rename(tmpPath, r.Path); err != nil {
		return fmt.Errorf("pindahkan hasil ke %s: %w", r.Filename, err)
	}
	r.released = true // penanda sudah tergantikan berkas asli
	return nil
}

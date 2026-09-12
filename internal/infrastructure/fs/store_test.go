package fs_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/fs"
)

func newStore(t *testing.T) *fs.Store {
	t.Helper()
	root := t.TempDir()
	s, err := fs.NewStore(filepath.Join(root, "output"), filepath.Join(root, "tmp"))
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return s
}

// Perilaku os.Rename di Windows menentukan apakah Commit boleh menimpa
// penanda reservasi. Diuji langsung alih-alih diasumsikan.
func TestRenameMenimpaBerkasYangSudahAda(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sumber")
	dst := filepath.Join(dir, "tujuan")

	if err := os.WriteFile(src, []byte("isi baru"), 0o644); err != nil {
		t.Fatalf("tulis sumber: %v", err)
	}
	if err := os.WriteFile(dst, []byte(""), 0o644); err != nil {
		t.Fatalf("tulis tujuan: %v", err)
	}

	if err := os.Rename(src, dst); err != nil {
		t.Fatalf("os.Rename menimpa berkas yang ada gagal: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("baca hasil: %v", err)
	}
	if string(got) != "isi baru" {
		t.Errorf("isi = %q, mau %q", got, "isi baru")
	}
}

func TestReserveDanCommit(t *testing.T) {
	s := newStore(t)

	r, err := s.Reserve("lagu.mp3")
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if r.Filename != "lagu.mp3" {
		t.Errorf("Filename = %q", r.Filename)
	}
	if _, err := os.Stat(r.Path); err != nil {
		t.Errorf("penanda reservasi tidak dibuat: %v", err)
	}

	tmp := filepath.Join(s.CommitTemp(), "hasil.mp3")
	if err := os.WriteFile(tmp, []byte("audio"), 0o644); err != nil {
		t.Fatalf("tulis temp: %v", err)
	}
	if err := s.Commit(tmp, r); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	got, err := os.ReadFile(r.Path)
	if err != nil {
		t.Fatalf("baca hasil: %v", err)
	}
	if string(got) != "audio" {
		t.Errorf("isi = %q, mau audio", got)
	}

	// Release setelah Commit tidak boleh menghapus berkas hasil.
	s.Release(r)
	if _, err := os.Stat(r.Path); err != nil {
		t.Errorf("berkas hasil terhapus oleh Release: %v", err)
	}
}

func TestReserveMemberiSufiksSaatTabrakan(t *testing.T) {
	s := newStore(t)

	first, err := s.Reserve("lagu.mp3")
	if err != nil {
		t.Fatalf("Reserve() pertama error = %v", err)
	}
	second, err := s.Reserve("lagu.mp3")
	if err != nil {
		t.Fatalf("Reserve() kedua error = %v", err)
	}

	if first.Filename == second.Filename {
		t.Fatalf("dua reservasi memenangkan nama sama: %q", first.Filename)
	}
	if second.Filename != "lagu (2).mp3" {
		t.Errorf("Filename kedua = %q, mau lagu (2).mp3", second.Filename)
	}
}

// Inilah alasan reservasi memakai O_EXCL: pengecekan naif "apakah berkas
// ada" menyisakan celah yang membuat dua worker menimpa hasil satu sama
// lain.
func TestReserveAmanSaatKontensi(t *testing.T) {
	s := newStore(t)
	const workers = 20

	var wg sync.WaitGroup
	names := make(chan string, workers)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := s.Reserve("sama.mp3")
			if err != nil {
				t.Errorf("Reserve() error = %v", err)
				return
			}
			names <- r.Filename
		}()
	}
	wg.Wait()
	close(names)

	seen := map[string]bool{}
	for name := range names {
		if seen[name] {
			t.Errorf("nama %q dimenangkan lebih dari satu goroutine", name)
		}
		seen[name] = true
	}
	if len(seen) != workers {
		t.Errorf("nama unik = %d, mau %d", len(seen), workers)
	}
}

func TestReleaseMenghapusPenandaKosong(t *testing.T) {
	s := newStore(t)

	r, err := s.Reserve("batal.mp3")
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	s.Release(r)

	if _, err := os.Stat(r.Path); !os.IsNotExist(err) {
		t.Error("penanda reservasi seharusnya terhapus")
	}

	// Nama itu harus bebas kembali.
	again, err := s.Reserve("batal.mp3")
	if err != nil {
		t.Fatalf("Reserve() ulang error = %v", err)
	}
	if again.Filename != "batal.mp3" {
		t.Errorf("Filename = %q, mau batal.mp3", again.Filename)
	}
}

func TestTempDirDanGC(t *testing.T) {
	s := newStore(t)

	dir, err := s.TempDirFor("job_1")
	if err != nil {
		t.Fatalf("TempDirFor() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bagian.part"), []byte("x"), 0o644); err != nil {
		t.Fatalf("tulis temp: %v", err)
	}

	aktif, err := s.TempDirFor("job_aktif")
	if err != nil {
		t.Fatalf("TempDirFor() error = %v", err)
	}

	// Buat keduanya tampak lama.
	past := time.Now().Add(-48 * time.Hour)
	for _, d := range []string{dir, aktif} {
		if err := os.Chtimes(d, past, past); err != nil {
			t.Fatalf("ubah waktu: %v", err)
		}
	}

	removed, err := s.GCTemp(24*time.Hour, map[string]bool{"job_aktif": true})
	if err != nil {
		t.Fatalf("GCTemp() error = %v", err)
	}
	if removed != 1 {
		t.Errorf("terhapus %d, mau 1", removed)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("temp yatim seharusnya terhapus")
	}
	if _, err := os.Stat(aktif); err != nil {
		t.Error("temp job aktif tidak boleh terhapus")
	}
}

func TestRemoveTempDir(t *testing.T) {
	s := newStore(t)
	if _, err := s.TempDirFor("job_1"); err != nil {
		t.Fatalf("TempDirFor() error = %v", err)
	}
	if err := s.RemoveTempDir("job_1"); err != nil {
		t.Fatalf("RemoveTempDir() error = %v", err)
	}
	// Idempoten: menghapus yang sudah tidak ada bukan error.
	if err := s.RemoveTempDir("job_1"); err != nil {
		t.Errorf("RemoveTempDir() kedua error = %v", err)
	}
}

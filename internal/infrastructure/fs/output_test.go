package fs_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Berkas commit sementara bernama "<id>-<acak>.mp3", bukan persis id job.
// Housekeeping berkala berjalan selagi job aktif, jadi berkas itu harus
// dikenali sebagai milik job aktif walau umurnya melewati batas.
func TestGCTempMelindungiBerkasJobAktif(t *testing.T) {
	s := newStore(t)
	old := time.Now().Add(-48 * time.Hour)

	workDir, err := s.TempDirFor("job_a")
	if err != nil {
		t.Fatalf("TempDirFor() error = %v", err)
	}
	paths := map[string]bool{
		workDir: true,
		filepath.Join(s.CommitTemp(), "job_a-123.mp3"):  true,  // milik job aktif
		filepath.Join(s.CommitTemp(), "job_ab-456.mp3"): false, // id lain berawalan sama
		filepath.Join(s.CommitTemp(), "job_x-789.mp3"):  false, // yatim
	}
	for p := range paths {
		if p != workDir {
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.GCTemp(24*time.Hour, map[string]bool{"job_a": true}); err != nil {
		t.Fatalf("GCTemp() error = %v", err)
	}

	for p, keep := range paths {
		_, err := os.Stat(p)
		if exists := err == nil; exists != keep {
			t.Errorf("%s ada = %v, mau %v", filepath.Base(p), exists, keep)
		}
	}
}

func TestSetOutputDirMenolakPathRelatif(t *testing.T) {
	s := newStore(t)
	before := s.OutputDir()

	if err := s.SetOutputDir(filepath.Join("relatif", "folder")); err == nil {
		t.Fatal("path relatif seharusnya ditolak")
	}
	// Kegagalan tidak boleh meninggalkan store tanpa tujuan.
	if s.OutputDir() != before {
		t.Errorf("direktori berubah jadi %q walau penggantian gagal", s.OutputDir())
	}
}

func TestSetOutputDirMembuatDirektoriBersarang(t *testing.T) {
	s := newStore(t)
	dir := filepath.Join(t.TempDir(), "baru", "bersarang")

	if err := s.SetOutputDir(dir); err != nil {
		t.Fatalf("SetOutputDir() error = %v", err)
	}
	if s.OutputDir() != dir {
		t.Errorf("OutputDir() = %q, mau %q", s.OutputDir(), dir)
	}

	entries, err := os.ReadDir(filepath.Join(dir, ".tmp"))
	if err != nil {
		t.Fatalf("direktori temp commit tidak dibuat: %v", err)
	}
	// Berkas uji tulis tidak boleh tertinggal.
	if len(entries) != 0 {
		t.Errorf("tersisa %d berkas di .tmp, mau 0", len(entries))
	}
}

// Job yang sedang berjalan memegang snapshot direktori lama. Bila snapshot
// ikut berganti, reservasi nama dan commit sebuah job bisa terbelah di dua
// volume berbeda, dan rename akhir gagal.
func TestSnapshotTetapSaatDirektoriBerganti(t *testing.T) {
	s := newStore(t)
	lama := s.Output()
	lamaDir := lama.Dir()

	if err := s.SetOutputDir(filepath.Join(t.TempDir(), "baru")); err != nil {
		t.Fatalf("SetOutputDir() error = %v", err)
	}

	if lama.Dir() != lamaDir {
		t.Fatalf("snapshot ikut berganti ke %q", lama.Dir())
	}
	if s.Output().Dir() == lamaDir {
		t.Fatal("job baru masih memakai direktori lama")
	}

	path, _, release, err := lama.ReservePath("lagu.mp3")
	if err != nil {
		t.Fatalf("ReservePath() error = %v", err)
	}
	defer release()

	if filepath.Dir(path) != lamaDir {
		t.Errorf("reservasi snapshot lama jatuh di %q, mau %q", filepath.Dir(path), lamaDir)
	}
}

package logging

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("baca %s: %v", filepath.Base(path), err)
	}
	return string(b)
}

func write(t *testing.T, d *DailyFile, s string) {
	t.Helper()
	if _, err := d.Write([]byte(s)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func TestRotasiSaatHariBerganti(t *testing.T) {
	dir := t.TempDir()
	c := &clock{t: day("2026-09-13 23:59")}

	d, err := OpenDaily(dir, 7, c.now)
	if err != nil {
		t.Fatalf("OpenDaily() error = %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	write(t, d, "baris kemarin\n")
	c.t = day("2026-09-14 00:01")
	write(t, d, "baris hari ini\n")

	if got := read(t, filepath.Join(dir, "app-2026-09-13.log")); got != "baris kemarin\n" {
		t.Errorf("arsip = %q", got)
	}
	if got := read(t, filepath.Join(dir, "app.log")); got != "baris hari ini\n" {
		t.Errorf("app.log = %q", got)
	}
}

// Aplikasi yang lama tidak dibuka tidak boleh terus menambahkan baris ke
// berkas milik hari pertama.
func TestBerkasLamaDiarsipkanSaatDibuka(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte("sesi lalu\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := day("2026-09-01 10:00")
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	c := &clock{t: day("2026-09-05 08:00")}
	d, err := OpenDaily(dir, 7, c.now)
	if err != nil {
		t.Fatalf("OpenDaily() error = %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	write(t, d, "sesi baru\n")

	if got := read(t, filepath.Join(dir, "app-2026-09-01.log")); got != "sesi lalu\n" {
		t.Errorf("arsip = %q", got)
	}
	if got := read(t, path); got != "sesi baru\n" {
		t.Errorf("app.log = %q", got)
	}
}

func TestArsipLewatMasaSimpanDibuang(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"app-2026-09-01.log", // lewat 7 hari
		"app-2026-09-06.log", // lewat 7 hari tepat di batas
		"app-2026-09-07.log", // masih disimpan
		"app-2026-09-12.log",
		"catatan-pribadi.log", // bukan milik kita
		"app-bukan-tanggal.log",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := &clock{t: day("2026-09-13 09:00")}
	d, err := OpenDaily(dir, 7, c.now)
	if err != nil {
		t.Fatalf("OpenDaily() error = %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	write(t, d, "hari ini\n")

	c.t = day("2026-09-14 09:00")
	write(t, d, "besok\n") // memicu rotasi dan pembersihan

	want := map[string]bool{
		"app-2026-09-01.log":    false,
		"app-2026-09-06.log":    false,
		"app-2026-09-07.log":    true,
		"app-2026-09-12.log":    true,
		"app-2026-09-13.log":    true,
		"catatan-pribadi.log":   true,
		"app-bukan-tanggal.log": true,
		"app.log":               true,
	}
	for name, exists := range want {
		_, err := os.Stat(filepath.Join(dir, name))
		if got := err == nil; got != exists {
			t.Errorf("%s ada = %v, mau %v", name, got, exists)
		}
	}
}

// Jam sistem yang mundur tidak boleh membuat arsip hari itu tertimpa.
func TestArsipBertanggalSamaDisambung(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app-2026-09-13.log"), []byte("pertama\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &clock{t: day("2026-09-13 22:00")}
	d, err := OpenDaily(dir, 7, c.now)
	if err != nil {
		t.Fatalf("OpenDaily() error = %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	write(t, d, "kedua\n")

	c.t = day("2026-09-14 01:00")
	write(t, d, "ketiga\n")

	if got := read(t, filepath.Join(dir, "app-2026-09-13.log")); got != "pertama\nkedua\n" {
		t.Errorf("arsip = %q, mau isi lama disambung", got)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("stderr tidak valid") }

// Tanpa console di Windows, stderr gagal ditulisi. Berkas log harus tetap
// terisi walau writer rusak berada di urutan pertama.
func TestTeeTetapMenulisWalauSatuTujuanRusak(t *testing.T) {
	var buf bytes.Buffer
	w := Tee(brokenWriter{}, &buf)

	n, err := w.Write([]byte("tetap tercatat\n"))
	if err != nil || n != len("tetap tercatat\n") {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	if !strings.Contains(buf.String(), "tetap tercatat") {
		t.Errorf("buf = %q", buf.String())
	}

	if _, err := Tee(brokenWriter{}).Write([]byte("x")); err == nil {
		t.Error("seluruh tujuan gagal harus mengembalikan error")
	}
}

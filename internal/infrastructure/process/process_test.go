package process_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
)

// Test ini menjalankan ulang binary test dirinya sendiri sebagai proses
// pembantu. Variabel lingkungan di bawah memilih peran, sehingga satu
// binary bisa berperan sebagai induk maupun cucu.
const (
	helperEnv    = "YT2MP3_PROC_HELPER"
	heartbeatEnv = "YT2MP3_PROC_HEARTBEAT"
)

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "parent":
		helperParent()
		return
	case "grandchild":
		helperGrandchild()
		return
	}
	os.Exit(m.Run())
}

// helperParent men-spawn cucu lalu menganggur.
//
// Cucu sengaja tidak ditunggu: ia menjadi yatim ketika induknya mati, persis
// seperti ffmpeg yang di-spawn yt-dlp. Membunuh induk saja tidak akan
// menghentikannya.
func helperParent() {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		helperEnv+"=grandchild",
		heartbeatEnv+"="+os.Getenv(heartbeatEnv),
	)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "spawn cucu gagal:", err)
		os.Exit(1)
	}
	fmt.Println("parent siap")
	time.Sleep(2 * time.Minute)
}

// helperGrandchild menulis detak ke berkas supaya test dapat memastikan
// apakah ia masih hidup, tanpa perlu pemeriksaan pid yang berbeda tiap OS.
func helperGrandchild() {
	path := os.Getenv(heartbeatEnv)
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString("x")
			_ = f.Close()
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func heartbeatSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// waitHeartbeat menunggu cucu benar-benar hidup.
func waitHeartbeat(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if heartbeatSize(t, path) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("cucu tidak pernah menulis detak")
}

// Inilah invarian paling penting di paket ini: membunuh job harus ikut
// mematikan cucu. Tanpa itu, cancel meninggalkan ffmpeg yang masih memegang
// berkas temp, dan di Windows berkas terkunci tidak bisa dihapus sehingga
// pembersihan ikut gagal.
func TestTerminateMematikanSeluruhProcessTree(t *testing.T) {
	heartbeat := filepath.Join(t.TempDir(), "detak.txt")

	h, err := process.Start(context.Background(), process.Spec{
		Bin: os.Args[0],
		Env: append(os.Environ(), helperEnv+"=parent", heartbeatEnv+"="+heartbeat),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	go func() { _, _ = io.Copy(io.Discard, h.Stdout) }()
	go func() { _, _ = io.Copy(io.Discard, h.Stderr) }()

	waitHeartbeat(t, heartbeat)

	if err := h.Terminate(time.Second); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	_ = h.Wait()

	// Beri jeda, lalu pastikan detak benar-benar berhenti bertambah.
	time.Sleep(500 * time.Millisecond)
	before := heartbeatSize(t, heartbeat)
	time.Sleep(time.Second)
	after := heartbeatSize(t, heartbeat)

	if after != before {
		t.Errorf("cucu masih hidup: detak bertambah dari %d ke %d", before, after)
	}
}

// Pembatalan context harus menempuh jalur terminasi yang sama.
func TestContextDibatalkanMematikanTree(t *testing.T) {
	heartbeat := filepath.Join(t.TempDir(), "detak.txt")
	ctx, cancel := context.WithCancel(context.Background())

	h, err := process.Start(ctx, process.Spec{
		Bin: os.Args[0],
		Env: append(os.Environ(), helperEnv+"=parent", heartbeatEnv+"="+heartbeat),
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, h.Stdout) }()
	go func() { _, _ = io.Copy(io.Discard, h.Stderr) }()

	waitHeartbeat(t, heartbeat)
	cancel()
	_ = h.Wait()

	time.Sleep(2 * time.Second)
	before := heartbeatSize(t, heartbeat)
	time.Sleep(time.Second)

	if after := heartbeatSize(t, heartbeat); after != before {
		t.Errorf("cucu masih hidup setelah context dibatalkan: %d -> %d", before, after)
	}
}

// Keluaran harus terbaca utuh: Wait menutup pipa, jadi tidak boleh ada
// jalur internal yang memanggilnya lebih dulu.
func TestKeluaranTerbacaUtuh(t *testing.T) {
	// Sengaja menjalankan test lain: mengarahkan ke test ini sendiri akan
	// membuat proses beranak tanpa henti sampai kena timeout.
	res, err := process.Output(context.Background(), process.Spec{
		Bin:     os.Args[0],
		Args:    []string{"-test.run", "^TestStartMenolakBinKosong$", "-test.v"},
		Env:     append(os.Environ(), helperEnv+"="),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if len(res.Stdout) == 0 {
		t.Error("stdout kosong; keluaran terpotong")
	}
}

func TestStartMenolakBinKosong(t *testing.T) {
	if _, err := process.Start(context.Background(), process.Spec{}); err == nil {
		t.Error("Start() dengan bin kosong seharusnya error")
	}
}

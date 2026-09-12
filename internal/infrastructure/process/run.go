package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"
)

// defaultGrace adalah jeda sebelum terminasi paksa.
const defaultGrace = 5 * time.Second

// Handle adalah proses anak yang sedang berjalan beserta pipa keluarannya.
//
// Pemilik handle wajib menghabiskan Stdout dan Stderr lalu memanggil Wait.
// Wait menutup kedua pipa, jadi memanggilnya sebelum pembacaan selesai akan
// memotong keluaran; itulah sebabnya tidak ada satu pun jalur internal di
// paket ini yang memanggil Wait sendiri.
type Handle struct {
	Stdout io.ReadCloser
	Stderr io.ReadCloser

	cmd   *exec.Cmd
	guard *guard

	waitOnce sync.Once
	waitErr  error
	exitCode int
	done     chan struct{}
}

// Start menjalankan proses anak dan mengembalikan handle untuk membaca
// keluarannya secara mengalir.
func Start(ctx context.Context, s Spec) (*Handle, error) {
	if s.Bin == "" {
		return nil, errors.New("bin kosong")
	}

	g, err := newGuard()
	if err != nil {
		return nil, fmt.Errorf("siapkan penjaga proses: %w", err)
	}

	// Sengaja bukan CommandContext: pembatalan ditangani Terminate supaya
	// seluruh process tree ikut mati, bukan hanya anak langsung.
	cmd := exec.Command(s.Bin, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	g.prepare(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		g.release()
		return nil, fmt.Errorf("pipa stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		g.release()
		return nil, fmt.Errorf("pipa stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		g.release()
		return nil, fmt.Errorf("jalankan %s: %w", s.Bin, err)
	}

	if err := g.adopt(cmd); err != nil {
		// Proses sudah terlanjur hidup; matikan daripada membiarkannya
		// berjalan di luar kendali.
		_ = cmd.Process.Kill()
		g.release()
		return nil, fmt.Errorf("adopsi proses: %w", err)
	}

	h := &Handle{
		Stdout: stdout,
		Stderr: stderr,
		cmd:    cmd,
		guard:  g,
		done:   make(chan struct{}),
	}

	// Pembatalan context memicu terminasi penuh. Goroutine ini hanya
	// memberi sinyal; penuaian proses tetap milik pemanggil Wait.
	go func() {
		select {
		case <-ctx.Done():
			_ = h.Terminate(defaultGrace)
		case <-h.done:
		}
	}()

	return h, nil
}

// Wait menunggu proses selesai dan menutup pipanya. Aman dipanggil lebih
// dari sekali; pemanggil kedua menerima hasil yang sama.
func (h *Handle) Wait() error {
	h.waitOnce.Do(func() {
		h.waitErr = h.cmd.Wait()
		if h.cmd.ProcessState != nil {
			h.exitCode = h.cmd.ProcessState.ExitCode()
		}
		h.guard.release()
		close(h.done)
	})
	<-h.done
	return h.waitErr
}

// ExitCode mengembalikan kode keluar; hanya berarti setelah Wait.
func (h *Handle) ExitCode() int {
	<-h.done
	return h.exitCode
}

// Terminate menghentikan proses beserta seluruh keturunannya.
//
// yt-dlp menjalankan ffmpeg sendiri untuk muxing, jadi membunuh anak
// langsung saja meninggalkan cucu yang masih memegang berkas temp. Di
// Windows berkas yang terkunci tidak bisa dihapus, sehingga pembersihan
// ikut gagal. Lihat ADR-012.
//
// Fungsi ini tidak menuai prosesnya: setelah kembali, pemilik handle tetap
// harus memanggil Wait.
func (h *Handle) Terminate(grace time.Duration) error {
	if h.cmd.Process == nil {
		return nil
	}

	// Sinyal lembut lebih dulu supaya tool sempat menutup berkasnya.
	_ = h.guard.signal(h.cmd)

	select {
	case <-h.done:
		return nil
	case <-time.After(grace):
	}

	if err := h.guard.kill(); err != nil {
		return fmt.Errorf("matikan process tree: %w", err)
	}
	return nil
}

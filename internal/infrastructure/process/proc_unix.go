//go:build !windows

package process

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

// guard menempatkan proses anak pada process group tersendiri.
//
// Dengan pgid sendiri, sinyal dapat dikirim ke seluruh grup sekaligus,
// sehingga cucu yang di-spawn yt-dlp ikut mati. Mengirim sinyal hanya ke
// anak langsung akan meninggalkan ffmpeg yatim yang masih memegang berkas
// temp. Lihat ADR-012.
type guard struct {
	pgid int
}

func newGuard() (*guard, error) {
	return &guard{}, nil
}

// prepare meminta kernel membuat process group baru saat proses lahir.
func (g *guard) prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// adopt mencatat pgid setelah proses hidup. Karena Setpgid dipasang sebelum
// fork, pgid selalu sama dengan pid anak.
func (g *guard) adopt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return errors.New("proses belum berjalan")
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Proses bisa saja sudah selesai sebelum sempat ditanyakan; pid
		// anak tetap pgid yang benar karena Setpgid dipasang sejak awal.
		pgid = cmd.Process.Pid
	}
	g.pgid = pgid
	return nil
}

// signal mengirim SIGTERM ke seluruh grup.
func (g *guard) signal(cmd *exec.Cmd) error {
	if g.pgid == 0 {
		return nil
	}
	if err := syscall.Kill(-g.pgid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("kirim SIGTERM ke grup %d: %w", g.pgid, err)
	}
	return nil
}

// kill mengirim SIGKILL ke seluruh grup.
func (g *guard) kill() error {
	if g.pgid == 0 {
		return nil
	}
	if err := syscall.Kill(-g.pgid, syscall.SIGKILL); err != nil && !isIgnorableKillError(err) {
		return fmt.Errorf("kirim SIGKILL ke grup %d: %w", g.pgid, err)
	}
	return nil
}

func isIgnorableKillError(err error) bool {
	return errors.Is(err, syscall.ESRCH) || errors.Is(err, syscall.EPERM)
}

// release tidak memegang sumber daya apa pun di Unix.
func (g *guard) release() {}

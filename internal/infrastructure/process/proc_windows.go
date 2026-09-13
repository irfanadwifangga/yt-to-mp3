//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// guard mengurung proses anak beserta keturunannya di dalam satu Job
// Object.
//
// Job Object dengan JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE adalah satu-satunya
// cara andal mematikan seluruh process tree di Windows: menutup handle job
// membunuh setiap proses di dalamnya, termasuk cucu yang di-spawn yt-dlp.
type guard struct {
	job windows.Handle

	once sync.Once
}

func newGuard() (*guard, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("buat job object: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	_, err = windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("atur job object: %w", err)
	}
	return &guard{job: job}, nil
}

// prepare menempatkan anak pada process group tersendiri, prasyarat untuk
// mengirim CTRL_BREAK tanpa ikut mematikan proses kita sendiri. Pada build
// tanpa console, jendela console milik anak ikut disembunyikan.
func (g *guard) prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: creationFlags(windows.CREATE_NEW_PROCESS_GROUP),
	}
}

// adopt memasukkan proses ke dalam job segera setelah ia hidup.
//
// Go tidak memberi akses ke handle thread utama, sehingga trik
// CREATE_SUSPENDED lalu ResumeThread tidak dapat dipakai. Konsekuensinya
// ada jendela sangat singkat antara Start dan adopsi; proses yang sempat
// di-spawn di jendela itu akan lolos dari job. Dalam praktiknya yt-dlp dan
// FFmpeg butuh puluhan milidetik untuk inisialisasi sebelum men-spawn apa
// pun, jadi jendela itu tidak pernah terpakai.
func (g *guard) adopt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("proses belum berjalan")
	}

	handle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("buka proses %d: %w", cmd.Process.Pid, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	if err := windows.AssignProcessToJobObject(g.job, handle); err != nil {
		return fmt.Errorf("masukkan ke job object: %w", err)
	}
	return nil
}

// signal mengirim CTRL_BREAK sebagai upaya terminasi lembut.
//
// Bersifat best effort: aplikasi tanpa console tidak dapat mengirimnya, dan
// tidak semua program menanggapinya. Kegagalan di sini tidak masalah karena
// kill() tetap menutup seluruh tree.
func (g *guard) signal(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(cmd.Process.Pid))
}

// kill menutup job, yang membunuh seluruh proses di dalamnya.
func (g *guard) kill() error {
	return g.closeJob()
}

// release melepaskan handle job. Karena KILL_ON_JOB_CLOSE aktif, ini juga
// memastikan tidak ada proses yang tertinggal ketika handle terakhir lepas.
func (g *guard) release() {
	_ = g.closeJob()
}

func (g *guard) closeJob() error {
	var err error
	g.once.Do(func() {
		if g.job != 0 {
			err = windows.CloseHandle(g.job)
			g.job = 0
		}
	})
	return err
}

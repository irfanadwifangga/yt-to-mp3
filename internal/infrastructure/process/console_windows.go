//go:build windows

package process

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

var procGetConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

// HasConsole melaporkan apakah proses ini terhubung ke jendela console.
//
// Build rilis Windows memakai subsystem GUI (-H windowsgui) supaya tidak
// membuka jendela console hitam saat dijalankan dari Start Menu. Saat
// dijalankan lewat terminal atau go run, console tetap ada.
func HasConsole() bool {
	hwnd, _, _ := procGetConsoleWindow.Call()
	return hwnd != 0
}

// creationFlags menambahkan CREATE_NO_WINDOW bila proses ini tidak punya
// console.
//
// Tanpa flag itu, setiap yt-dlp, FFmpeg, dan probe versi yang dijalankan
// aplikasi GUI membuka jendela console sendiri yang berkedip di layar. Bila
// console ada, anak mewarisinya sehingga tidak ada jendela baru, dan
// CTRL_BREAK untuk terminasi lembut tetap sampai.
func creationFlags(base uint32) uint32 {
	if HasConsole() {
		return base
	}
	return base | windows.CREATE_NO_WINDOW
}

// hideConsole dipakai pemanggilan sekali jalan yang tidak butuh process
// group tersendiri.
func hideConsole(cmd *exec.Cmd) {
	if flags := creationFlags(0); flags != 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: flags}
	}
}

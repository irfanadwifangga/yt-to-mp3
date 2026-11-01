//go:build windows

package browser

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"syscall"
	"unsafe"
)

// WM_CLOSE lets Chromium flush its dedicated profile before exiting.
func closeAppProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	user32 := windows.NewLazySystemDLL("user32.dll")
	enum := user32.NewProc("EnumWindows")
	getPID := user32.NewProc("GetWindowThreadProcessId")
	post := user32.NewProc("PostMessageW")
	found := false
	callback := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		getPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == uint32(process.Pid) {
			result, _, _ := post.Call(hwnd, 0x0010, 0, 0)
			if result != 0 {
				found = true
			}
		}
		return 1
	})
	enum.Call(callback, 0)
	if found {
		return nil
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

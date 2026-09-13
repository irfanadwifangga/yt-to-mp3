// Package browser membuka URL di browser default pengguna dan lokasi berkas
// di file manager.
package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Open meluncurkan browser default. Perintah dijalankan sebagai argv,
// tidak pernah lewat shell.
func Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return start(cmd, "buka browser")
}

// start menjalankan perintah lalu melepasnya; aplikasi yang dibuka tidak
// ditunggu.
func start(cmd *exec.Cmd, what string) error {
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

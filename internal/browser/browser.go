// Package browser membuka URL di browser default pengguna.
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
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("buka browser: %w", err)
	}
	// Proses browser sengaja dilepas; kita tidak menunggunya.
	go func() { _ = cmd.Wait() }()
	return nil
}

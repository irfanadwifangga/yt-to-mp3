// Package browser membuka URL di browser default pengguna.
package browser

import (
	"fmt"
	"os/exec"
	"path/filepath"
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

// Reveal membuka file manager pada lokasi sebuah berkas, dengan berkasnya
// tersorot bila sistem mendukung.
//
// Path berasal dari database, bukan dari klien: endpoint yang menerima path
// dari luar akan menjadi jalur membuka berkas sewenang-wenang.
func Reveal(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// Explorer memerlukan bentuk "/select,<path>" sebagai satu argumen.
		cmd = exec.Command("explorer", "/select,"+filepath.Clean(path))
	case "darwin":
		cmd = exec.Command("open", "-R", path)
	default:
		// Sebagian besar file manager Linux tidak punya opsi sorot yang
		// seragam, jadi cukup buka direktorinya.
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("buka lokasi berkas: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

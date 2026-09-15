//go:build windows

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// OpenAppWindow membuka URL sebagai jendela aplikasi Microsoft Edge: jendela
// sendiri tanpa tab maupun address bar. Edge terpasang di setiap Windows 10
// dan 11, jadi tidak ada dependensi yang perlu dibawa aplikasi. Lihat
// ADR-034.
func OpenAppWindow(url, profileDir string) (*Window, error) {
	edge := findEdge()
	if edge == "" {
		return nil, ErrAppWindowUnavailable
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, fmt.Errorf("siapkan profil jendela: %w", err)
	}

	cmd := exec.Command(edge, appWindowArgs(url, profileDir)...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("buka jendela aplikasi: %w", err)
	}
	return watch(cmd), nil
}

// findEdge mencari msedge.exe lewat registry App Paths, lalu lokasi
// pemasangan yang umum. String kosong berarti Edge tidak ditemukan.
func findEdge() string {
	const appPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\msedge.exe`
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		k, err := registry.OpenKey(root, appPath, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		path, _, err := k.GetStringValue("")
		_ = k.Close()
		if err == nil && isFile(path) {
			return path
		}
	}

	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		path := filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe")
		if isFile(path) {
			return path
		}
	}
	return ""
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

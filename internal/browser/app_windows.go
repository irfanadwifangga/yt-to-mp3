//go:build windows

package browser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// OpenAppWindow membuka URL sebagai jendela aplikasi browser Chromium (Edge,
// Brave, Chrome, dll): jendela sendiri tanpa tab maupun address bar.
// Menggunakan browser Chromium yang tersedia di sistem dengan urutan prioritas
// Edge, Brave, Chrome, Chromium, dan Vivaldi. Lihat ADR-034.
func OpenAppWindow(url, profileDir string) (*Window, error) {
	browserPath := findAppBrowser()
	if browserPath == "" {
		return nil, ErrAppWindowUnavailable
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, fmt.Errorf("siapkan profil jendela: %w", err)
	}

	cmd := exec.Command(browserPath, appWindowArgs(url, profileDir)...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("buka jendela aplikasi: %w", err)
	}
	return watch(cmd), nil
}

type browserTarget struct {
	regKey  string
	subPath string
}

var chromiumBrowsers = []browserTarget{
	{regKey: "msedge.exe", subPath: filepath.Join("Microsoft", "Edge", "Application", "msedge.exe")},
	{regKey: "brave.exe", subPath: filepath.Join("BraveSoftware", "Brave-Browser", "Application", "brave.exe")},
	{regKey: "chrome.exe", subPath: filepath.Join("Google", "Chrome", "Application", "chrome.exe")},
	{regKey: "chromium.exe", subPath: filepath.Join("Chromium", "Application", "chrome.exe")},
	{regKey: "vivaldi.exe", subPath: filepath.Join("Vivaldi", "Application", "vivaldi.exe")},
}

// findAppBrowser mencari executable browser berbasis Chromium yang dapat
// menjalankan jendela aplikasi (--app). Memeriksa Edge, Brave, Chrome, dll.
func findAppBrowser() string {
	for _, b := range chromiumBrowsers {
		if path := findBrowserExe(b.regKey, b.subPath); path != "" {
			return path
		}
	}
	return ""
}

// findEdge mencari msedge.exe untuk kompatibilitas.
func findEdge() string {
	return findBrowserExe("msedge.exe", filepath.Join("Microsoft", "Edge", "Application", "msedge.exe"))
}

func findBrowserExe(regName, relPath string) string {
	appPath := `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\` + regName
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

	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		path := filepath.Join(base, relPath)
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

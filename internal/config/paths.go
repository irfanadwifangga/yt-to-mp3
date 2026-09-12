package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
)

// Paths memuat seluruh lokasi yang dipakai aplikasi. Lihat docs planning
// "Storage layout dan konfigurasi".
type Paths struct {
	DataDir     string
	DBDir       string
	ToolsDir    string
	TempDir     string
	LogDir      string
	OutputDir   string
	ConfigFile  string
	RuntimeFile string
}

// resolveDataDir mengembalikan direktori data per OS.
func resolveDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA tidak diset")
		}
		return filepath.Join(base, version.AppName), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", version.AppName), nil
	default:
		if base := os.Getenv("XDG_DATA_HOME"); base != "" {
			return filepath.Join(base, version.AppName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", version.AppName), nil
	}
}

// resolveOutputDir mengembalikan direktori output default per OS.
func resolveOutputDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "linux" {
		if dir := os.Getenv("XDG_MUSIC_DIR"); dir != "" {
			return filepath.Join(dir, version.AppName), nil
		}
	}
	return filepath.Join(home, "Music", version.AppName), nil
}

// newPaths menyusun Paths dan membuat direktori yang belum ada.
func newPaths(outputOverride string) (Paths, error) {
	dataDir, err := resolveDataDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve data dir: %w", err)
	}

	outputDir := outputOverride
	if outputDir == "" {
		outputDir, err = resolveOutputDir()
		if err != nil {
			return Paths{}, fmt.Errorf("resolve output dir: %w", err)
		}
	}

	p := Paths{
		DataDir:     dataDir,
		DBDir:       filepath.Join(dataDir, "db"),
		ToolsDir:    filepath.Join(dataDir, "tools"),
		TempDir:     filepath.Join(dataDir, "tmp"),
		LogDir:      filepath.Join(dataDir, "logs"),
		OutputDir:   outputDir,
		ConfigFile:  filepath.Join(dataDir, "config.json"),
		RuntimeFile: filepath.Join(dataDir, "runtime.json"),
	}

	// Temp untuk commit final harus satu volume dengan output supaya rename
	// tetap atomik. Lihat docs planning "Verifikasi dan commit".
	dirs := []string{p.DataDir, p.DBDir, p.ToolsDir, p.TempDir, p.LogDir, p.OutputDir,
		filepath.Join(p.OutputDir, ".tmp")}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return Paths{}, fmt.Errorf("buat direktori %s: %w", d, err)
		}
	}
	return p, nil
}

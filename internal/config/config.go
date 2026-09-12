// Package config memuat konfigurasi aplikasi dan lokasi penyimpanan.
//
// Presedensi: default built-in -> config.json -> env var. Nilai dari tabel
// settings (yang diubah lewat UI) menyusul pada tahap persistence.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// Config memuat kunci yang dapat diubah pengguna.
type Config struct {
	OutputDir           string `json:"output_dir"`
	MaxConcurrentJobs   int    `json:"max_concurrent_jobs"`
	MaxQueueDepth       int    `json:"max_queue_depth"`
	DefaultPresetID     string `json:"default_preset_id"`
	FilenameMode        string `json:"filename_mode"`
	ToolUpdateCheck     bool   `json:"tool_update_check"`
	IdleShutdownMinutes int    `json:"idle_shutdown_minutes"`
	LogLevel            string `json:"log_level"`

	Paths Paths `json:"-"`
}

// Default mengembalikan konfigurasi bawaan. Concurrency sengaja rendah:
// lebih dari 2-3 unduhan paralel dari satu IP memicu throttling.
func Default() Config {
	return Config{
		MaxConcurrentJobs:   2,
		MaxQueueDepth:       50,
		DefaultPresetID:     "mp3_standard",
		FilenameMode:        "title",
		ToolUpdateCheck:     true,
		IdleShutdownMinutes: 30,
		LogLevel:            "info",
	}
}

// Load membaca konfigurasi, menulis file default bila belum ada, lalu
// menyiapkan seluruh direktori yang dibutuhkan.
func Load() (Config, error) {
	cfg := Default()

	dataDir, err := resolveDataDir()
	if err != nil {
		return Config{}, err
	}
	configFile := filepath.Join(dataDir, "config.json")

	var missing bool
	raw, err := os.ReadFile(configFile)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("baca %s: %w", configFile, err)
		}
	case errors.Is(err, os.ErrNotExist):
		missing = true // ditulis setelah direktori tersedia
	default:
		return Config{}, fmt.Errorf("baca %s: %w", configFile, err)
	}

	applyEnv(&cfg)

	paths, err := newPaths(cfg.OutputDir)
	if err != nil {
		return Config{}, err
	}
	cfg.Paths = paths
	cfg.OutputDir = paths.OutputDir

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	if missing {
		if err := cfg.Save(); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// applyEnv menimpa konfigurasi dengan env var berprefix YT2MP3_.
func applyEnv(cfg *Config) {
	if v := os.Getenv("YT2MP3_OUTPUT_DIR"); v != "" {
		cfg.OutputDir = v
	}
	if v := os.Getenv("YT2MP3_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("YT2MP3_MAX_CONCURRENT_JOBS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrentJobs = n
		}
	}
}

// Validate menolak nilai yang tidak masuk akal lebih awal.
func (c Config) Validate() error {
	if c.MaxConcurrentJobs < 1 || c.MaxConcurrentJobs > 8 {
		return fmt.Errorf("max_concurrent_jobs harus 1..8, dapat %d", c.MaxConcurrentJobs)
	}
	if c.MaxQueueDepth < 1 {
		return fmt.Errorf("max_queue_depth harus >= 1, dapat %d", c.MaxQueueDepth)
	}
	switch c.FilenameMode {
	case "title", "title-uploader", "uploader-title", "id":
	default:
		return fmt.Errorf("filename_mode tidak dikenal: %q", c.FilenameMode)
	}
	return nil
}

// Save menulis konfigurasi secara atomik.
func (c Config) Save() error {
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.Paths.ConfigFile + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.Paths.ConfigFile)
}

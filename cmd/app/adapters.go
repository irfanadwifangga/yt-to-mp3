package main

import (
	"strconv"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
)

// Adapter infrastructure ke port application ada di internal/adapters,
// supaya test E2E memakai adapter yang sama dengan aplikasi. Yang tersisa
// di sini hanya pemetaan yang khusus untuk proses ini.

// settingDefaults memetakan konfigurasi startup jadi nilai bawaan setelan.
//
// Nilai ini yang berlaku ketika pengguna belum pernah mengubah apa pun,
// sehingga default baru di versi berikutnya tetap sampai ke pengguna yang
// tidak menyentuh setelan tersebut.
func settingDefaults(cfg config.Config) map[string]string {
	return map[string]string{
		application.KeyOutputDir:       cfg.OutputDir,
		application.KeyMaxConcurrent:   strconv.Itoa(cfg.MaxConcurrentJobs),
		application.KeyMaxQueueDepth:   strconv.Itoa(cfg.MaxQueueDepth),
		application.KeyDefaultPreset:   cfg.DefaultPresetID,
		application.KeyFilenameMode:    cfg.FilenameMode,
		application.KeyToolUpdateCheck: strconv.FormatBool(cfg.ToolUpdateCheck),
		application.KeyIdleShutdown:    strconv.Itoa(cfg.IdleShutdownMinutes),
		application.KeyLogLevel:        cfg.LogLevel,
	}
}

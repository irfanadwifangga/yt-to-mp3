// Command app menjalankan server lokal yt-to-mp3.
//
// Seluruh wiring dependensi terjadi di sini; paket lain tidak saling
// merakit. Lihat docs architecture "Prinsip dan aturan dependensi".
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/browser"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ytdlp"
	"github.com/irfanadwifangga/yt-to-mp3/internal/instance"
	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
	"github.com/irfanadwifangga/yt-to-mp3/internal/worker"
	"github.com/irfanadwifangga/yt-to-mp3/web"
)

const shutdownGrace = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		port        = flag.Int("port", 0, "port TCP; 0 berarti acak (disarankan)")
		noBrowser   = flag.Bool("no-browser", false, "jangan buka browser saat start")
		devMode     = flag.Bool("dev", false, "izinkan origin Vite dev server (hanya untuk pengembangan)")
		showVersion = flag.Bool("version", false, "tampilkan versi lalu keluar")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s (%s)\n", version.AppName, version.Version, version.Commit)
		return nil
	}

	// Dibuat sejak awal supaya pekerjaan startup yang lama (migrasi, probe
	// tool) ikut dapat dibatalkan dengan Ctrl+C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("muat konfigurasi: %w", err)
	}

	log := newLogger(cfg.LogLevel)

	// Single instance: bila instance lama masih menjawab, cukup buka
	// browsernya dan keluar. Lihat ADR-022.
	if info, running := instance.FindRunning(cfg.Paths.RuntimeFile); running {
		log.Info("instance lain sudah berjalan", "pid", info.PID, "port", info.Port)
		if !*noBrowser {
			if err := browser.Open(info.URL()); err != nil {
				log.Warn("gagal membuka browser", "error", err)
			}
		}
		return nil
	}

	token, err := api.NewToken()
	if err != nil {
		return err
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("listen loopback: %w", err)
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	spaFS, err := web.Dist()
	if err != nil {
		return fmt.Errorf("baca SPA ter-embed: %w", err)
	}
	spaBuilt := web.Built()
	if !spaBuilt {
		log.Warn("SPA belum di-build, menyajikan halaman petunjuk")
	}

	// Discovery tool tidak boleh menghalangi startup: tool yang belum ada
	// dilaporkan lewat /api/health dan dapat dipasang dari UI.
	toolManager, err := tools.New(cfg.Paths.ToolsDir, cfg.Paths.TempDir, log)
	if err != nil {
		return fmt.Errorf("siapkan tool manager: %w", err)
	}

	database, err := db.Open(ctx, filepath.Join(cfg.Paths.DBDir, "app.db"), log)
	if err != nil {
		return fmt.Errorf("buka database: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil {
			log.Warn("tutup database gagal", "error", err)
		}
	}()

	if err := database.Migrate(ctx, version.Version); err != nil {
		return fmt.Errorf("migrasi database: %w", err)
	}

	jobs := db.NewJobRepository(database)

	// Recovery dijalankan sebelum listener dibuka. Job berstatus aktif di
	// database berarti proses pemiliknya sudah mati bersama sesi sebelumnya,
	// jadi tidak mungkin dilanjutkan.
	swept, err := jobs.SweepNonTerminal(ctx)
	if err != nil {
		return fmt.Errorf("crash recovery: %w", err)
	}
	if swept > 0 {
		log.Warn("job tertinggal dari sesi sebelumnya ditandai gagal", "jumlah", swept)
	}

	presets := db.NewPresetRepository(database)
	mediaCache := db.NewMediaRepository(database)
	resolver := ytdlp.NewResolver(toolManager, log)

	hub := api.NewHub(jobs, log)

	// Pipeline unduhan dan transcode menyusul pada tahap 5; sampai saat itu
	// mesin antrean tetap utuh dan job berakhir gagal dengan sebab jelas.
	scheduler := worker.New(jobs, pendingPipeline{}, hub, cfg.MaxConcurrentJobs, log)

	jobService := application.NewJobService(
		jobs, presets, mediaCache, resolver, hub, scheduler.Notify(),
		application.JobServiceConfig{
			MaxQueueDepth:   cfg.MaxQueueDepth,
			DefaultPreset:   cfg.DefaultPresetID,
			DefaultFilename: domain.FilenameMode(cfg.FilenameMode),
		}, log)

	go scheduler.Run(ctx)

	srv := api.New(api.Options{
		Config:    cfg,
		Logger:    log,
		Token:     token,
		Port:      actualPort,
		SPA:       spaFS,
		SPABuilt:  spaBuilt,
		Dev:       *devMode,
		Tools:     toolManager,
		Presets:   presets,
		Metadata:  application.NewMetadataService(resolver, mediaCache, log),
		Jobs:      jobService,
		Canceller: scheduler,
		Hub:       hub,
	})

	info := instance.Info{
		PID:       os.Getpid(),
		Port:      actualPort,
		Token:     token,
		StartedAt: time.Now().UTC(),
		Version:   version.Version,
	}
	if err := instance.Write(cfg.Paths.RuntimeFile, info); err != nil {
		return fmt.Errorf("tulis runtime.json: %w", err)
	}
	defer func() {
		if err := instance.Remove(cfg.Paths.RuntimeFile); err != nil {
			log.Warn("gagal menghapus runtime.json", "error", err)
		}
	}()

	httpSrv := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	log.Info("siap",
		"version", version.Version,
		"url", info.URL(),
		"data_dir", cfg.Paths.DataDir,
		"output_dir", cfg.OutputDir)

	if !*noBrowser {
		if err := browser.Open(info.URL()); err != nil {
			log.Warn("gagal membuka browser", "error", err)
		}
	}

	select {
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		log.Info("sinyal diterima, mematikan")
	case <-srv.ShutdownRequested():
		log.Info("permintaan shutdown dari UI")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("berhenti dengan bersih")
	return nil
}

// newLogger menyiapkan structured logging ke stderr.
func newLogger(level string) *slog.Logger {
	var lv slog.Level
	if err := lv.UnmarshalText([]byte(level)); err != nil {
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lv}))
}

// pendingPipeline adalah JobRunner sementara untuk tahap 4.
//
// Mesin antrean sudah lengkap, tetapi isi pekerjaannya (yt-dlp dan FFmpeg)
// baru hadir pada tahap 5. Sampai saat itu job berakhir gagal dengan sebab
// yang jelas alih-alih menggantung, dan seluruh jalur transisi, pembatalan,
// serta siaran event tetap berjalan sungguhan. Hapus tipe ini ketika
// pipeline sudah ada.
type pendingPipeline struct{}

func (pendingPipeline) Run(context.Context, *domain.Job) error {
	return domain.NewError(domain.CodeInternal, domain.ClassPermanent,
		"pipeline unduhan belum tersedia pada versi ini")
}

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
	"strconv"
	"syscall"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/adapters"
	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/browser"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
	"github.com/irfanadwifangga/yt-to-mp3/internal/dialog"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ffmpeg"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/fs"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ytdlp"
	"github.com/irfanadwifangga/yt-to-mp3/internal/instance"
	"github.com/irfanadwifangga/yt-to-mp3/internal/logging"
	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
	"github.com/irfanadwifangga/yt-to-mp3/internal/worker"
	"github.com/irfanadwifangga/yt-to-mp3/web"
)

const shutdownGrace = 10 * time.Second

// logRetentionDays adalah jumlah arsip log harian yang disimpan.
const logRetentionDays = 7

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		// Build rilis Windows berjalan tanpa console (-H windowsgui), jadi
		// stderr tidak terlihat siapa pun. Tanpa dialog, kegagalan startup
		// membuat aplikasi seolah tidak pernah dibuka.
		if !process.HasConsole() {
			dialog.Error(version.AppName,
				"yt-to-mp3 gagal dijalankan / failed to start.\n\n"+err.Error())
		}
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

	// Tingkat log dipegang LevelVar supaya bisa diganti selagi berjalan,
	// termasuk setelah setelan tersimpan dibaca dari database.
	var logLevel slog.LevelVar
	if err := applyLogLevel(&logLevel, cfg.LogLevel); err != nil {
		logLevel.Set(slog.LevelInfo)
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: &logLevel}))

	// Single instance: bila instance lama masih menjawab, cukup buka
	// browsernya dan keluar. Lihat ADR-022.
	if info, running := instance.FindRunning(cfg.Paths.RuntimeFile); running {
		log.Info("instance lain sudah berjalan", "pid", info.PID, "port", info.Port)
		if !*noBrowser {
			openBrowser(info.URL(), log, true)
		}
		return nil
	}

	// Berkas log baru dibuka setelah dipastikan tidak ada instance lain.
	// Instance kedua hanya hidup sekejap, dan membiarkannya ikut merotasi
	// app.log milik instance pertama tidak memberi apa pun.
	logFile, err := logging.OpenDaily(cfg.Paths.LogDir, logRetentionDays, nil)
	if err != nil {
		log.Warn("berkas log tidak dapat dibuka, log hanya ke terminal", "error", err)
	} else {
		defer func() { _ = logFile.Close() }()
		log = slog.New(slog.NewTextHandler(logging.Tee(logFile, os.Stderr),
			&slog.HandlerOptions{Level: &logLevel}))
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

	// Setelan dimuat sebelum komponen yang membacanya disusun. Sebelumnya
	// store, scheduler, dan logger dibentuk dari config.json lebih dulu,
	// sehingga nilai yang disimpan pengguna lewat UI tidak pernah berlaku,
	// bahkan setelah restart.
	settings := application.NewSettingsService(
		db.NewSettingsRepository(database), presets, settingDefaults(cfg))
	if err := settings.Load(ctx); err != nil {
		return fmt.Errorf("muat setelan: %w", err)
	}
	effective, err := settings.Effective(ctx)
	if err != nil {
		return fmt.Errorf("baca setelan: %w", err)
	}

	if err := applyLogLevel(&logLevel, effective[application.KeyLogLevel]); err != nil {
		log.Warn("tingkat log tersimpan tidak dikenal, memakai bawaan", "error", err)
	}

	store, err := openStore(effective[application.KeyOutputDir], cfg, log)
	if err != nil {
		return err
	}

	// Direktori keluaran dan tingkat log diterapkan seketika saat diubah.
	settings.SetApplier(application.KeyOutputDir, store.SetOutputDir)
	settings.SetApplier(application.KeyLogLevel, func(v string) error {
		return applyLogLevel(&logLevel, v)
	})

	hub := api.NewHub(jobs, log)
	files := db.NewFileRepository(database)

	pipeline := application.NewPipeline(application.PipelineDeps{
		Repo:       jobs,
		Closer:     jobs,
		Presets:    presets,
		Cache:      mediaCache,
		Resolver:   resolver,
		Downloader: adapters.Downloader{Inner: ytdlp.NewDownloader(toolManager, log)},
		Transcoder: adapters.Transcoder{Inner: ffmpeg.NewTranscoder(toolManager, log)},
		Verifier:   ffmpeg.NewProber(toolManager, log),
		Store:      store,
		Naming:     adapters.Naming{},
		Events:     hub,
		Log:        log,
	})

	concurrency, err := strconv.Atoi(effective[application.KeyMaxConcurrent])
	if err != nil {
		concurrency = cfg.MaxConcurrentJobs
	}
	scheduler := worker.New(jobs, pipeline, hub, concurrency, log)

	jobService := application.NewJobService(
		jobs, presets, mediaCache, resolver, files, hub, scheduler.Notify(),
		settings.Live, log)

	// Putaran pertama berjalan sebelum scheduler dan listener: sisa temp dari
	// sesi yang mati mendadak dibersihkan, dan berkas yang dihapus selagi
	// aplikasi tertutup sudah ditandai sebelum riwayat pertama kali dibaca.
	housekeeper := application.NewHousekeeper(application.HousekeepingDeps{
		Events:     jobs,
		Media:      mediaCache,
		Files:      files,
		Temp:       store,
		ActiveJobs: scheduler.RunningIDs,
		Log:        log,
	})
	housekeeper.RunOnce(ctx)

	go scheduler.Run(ctx)
	go housekeeper.Run(ctx)

	// Cek pembaruan hanya membaca versi terbaru; pemasangan selalu menunggu
	// permintaan eksplisit dari UI.
	toolService := application.NewToolService(toolManager,
		tools.NewUpdateStateFile(filepath.Join(cfg.Paths.DataDir, "tool-updates.json")),
		func() bool { return settings.Live().ToolUpdateCheck }, log)
	toolService.SetApp(version.Version, version.ReleasesURL)
	go toolService.Run(ctx)

	srv := api.New(api.Options{
		Config:    cfg,
		Logger:    log,
		Token:     token,
		Port:      actualPort,
		SPA:       spaFS,
		SPABuilt:  spaBuilt,
		Dev:       *devMode,
		Tools:     toolService,
		Presets:   presets,
		Metadata:  application.NewMetadataService(resolver, mediaCache, log),
		Jobs:      jobService,
		Canceller: scheduler,
		Hub:       hub,
		Files:     files,
		Settings:  settings,
		Revealer:  adapters.Revealer{},
		Picker:    adapters.Picker{},
		OutputDir: store.OutputDir,
	})

	idle := application.NewIdleMonitor(application.IdleDeps{
		LastActivity: srv.LastActivity,
		OpenStreams:  hub.Streams,
		Jobs:         jobs,
		Timeout: func() time.Duration {
			return time.Duration(settings.Live().IdleShutdownMinutes) * time.Minute
		},
		Log: log,
	})
	// Mode dev menjalankan backend tanpa tab yang terus polling selama
	// frontend dikembangkan; berhenti sendiri di sana hanya mengganggu.
	if *devMode {
		log.Info("idle shutdown dimatikan pada mode dev")
	} else {
		go idle.Run(ctx)
	}

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
	// Stream SSE tidak pernah selesai sendiri, jadi harus diakhiri saat
	// shutdown dimulai; kalau tidak, Shutdown menunggu sampai batas waktunya.
	httpSrv.RegisterOnShutdown(srv.CloseStreams)

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
		"port", actualPort,
		"data_dir", cfg.Paths.DataDir,
		"output_dir", store.OutputDir(),
		"job_paralel", concurrency)

	// URL bertoken hanya dicetak ke terminal, tidak pernah ke berkas log:
	// berkas log lazim dilampirkan pada laporan bug, sedangkan token itu
	// memberi akses penuh ke API selama proses hidup.
	fmt.Fprintln(os.Stderr, "buka:", info.URL())

	if !*noBrowser {
		openBrowser(info.URL(), log, false)
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
	case <-idle.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("berhenti dengan bersih")
	return nil
}

// openBrowser membuka URL di browser default.
//
// Pada build tanpa console, baris "buka:" di stderr tidak terlihat, sehingga
// browser yang gagal dibuka membuat aplikasi tidak bisa dijangkau sama
// sekali. Alamatnya ditampilkan lewat dialog sebagai gantinya. wait menahan
// sampai dialog ditutup, untuk jalur yang langsung keluar sesudahnya.
func openBrowser(url string, log *slog.Logger, wait bool) {
	err := browser.Open(url)
	if err == nil {
		return
	}
	log.Warn("gagal membuka browser", "error", err)
	if process.HasConsole() {
		return
	}

	show := func() {
		dialog.Info(version.AppName,
			"Browser tidak dapat dibuka otomatis. Salin alamat ini ke browser:\n"+
				"The browser could not be opened. Copy this address into a browser:\n\n"+url)
	}
	if wait {
		show()
		return
	}
	go show()
}

// openStore memakai direktori keluaran tersimpan, dengan bawaan config
// sebagai cadangan.
//
// Folder yang dipilih pengguna bisa saja hilang di antara dua sesi, misalnya
// drive eksternal yang dilepas. Aplikasi yang menolak start karena itu tidak
// memberi pengguna jalan untuk memilih folder baru.
func openStore(dir string, cfg config.Config, log *slog.Logger) (*fs.Store, error) {
	store, err := fs.NewStore(dir, cfg.Paths.TempDir)
	if err == nil {
		return store, nil
	}
	if dir == cfg.OutputDir {
		return nil, fmt.Errorf("siapkan direktori keluaran: %w", err)
	}

	log.Warn("direktori keluaran tersimpan tidak dapat dipakai, memakai bawaan",
		"tersimpan", dir, "bawaan", cfg.OutputDir, "error", err)
	store, err = fs.NewStore(cfg.OutputDir, cfg.Paths.TempDir)
	if err != nil {
		return nil, fmt.Errorf("siapkan direktori keluaran: %w", err)
	}
	return store, nil
}

// applyLogLevel memasang tingkat log dari teks seperti "info" atau "debug".
func applyLogLevel(lv *slog.LevelVar, value string) error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(value)); err != nil {
		return fmt.Errorf("tingkat log %q tidak dikenal", value)
	}
	lv.Set(level)
	return nil
}

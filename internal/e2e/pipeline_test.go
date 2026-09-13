//go:build integration

// Package e2e_test menjalankan jalur job lengkap tanpa jaringan: JobService,
// scheduler, pipeline, SQLite, store berkas, dan FFmpeg sungguhan. Satu-
// satunya yang dipalsukan adalah yt-dlp, diperankan oleh binary test ini
// sendiri, sehingga tidak ada URL publik yang dijadikan oracle (planning
// §23).
//
//	go test -tags integration -run E2E ./internal/e2e/
//
// FFmpeg dicari seperti aplikasi mencarinya; tanpa FFmpeg test dilewati,
// kecuali YT2MP3_REQUIRE_TOOLS=1 yang membuatnya gagal.
package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ffmpeg"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/fs"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ytdlp"
	"github.com/irfanadwifangga/yt-to-mp3/internal/worker"
)

// Variabel lingkungan yang mengendalikan yt-dlp palsu. Proses anak mewarisi
// env induk, jadi t.Setenv cukup untuk mengatur perilaku per test.
const (
	roleEnv   = "YT2MP3_E2E_ROLE"
	modeEnv   = "YT2MP3_E2E_MODE"
	audioEnv  = "YT2MP3_E2E_AUDIO"
	markerEnv = "YT2MP3_E2E_MARKER"
)

const (
	testURL   = "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	testTitle = "Nada Uji E2E"
)

func TestMain(m *testing.M) {
	if os.Getenv(roleEnv) == "ytdlp" {
		os.Exit(fakeYTDLP(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// fakeYTDLP meniru antarmuka baris perintah yt-dlp yang dipakai aplikasi.
//
//	ok         menulis audio fixture sebagai hasil unduhan
//	fail-once  gagal sementara pada panggilan unduh pertama, lalu berhasil
//	slow       mulai mengunduh lalu menggantung, untuk menguji pembatalan
func fakeYTDLP(args []string) int {
	if slices.Contains(args, "--version") {
		fmt.Println("2026.01.01")
		return 0
	}
	if slices.Contains(args, "--dump-single-json") {
		fmt.Printf(`{"id":"dQw4w9WgXcQ","title":%q,"uploader":"Kanal Uji","duration":5,"acodec":"opus","asr":48000}`+"\n", testTitle)
		return 0
	}

	var template string
	for i, a := range args {
		if a == "-o" && i+1 < len(args) {
			template = args[i+1]
		}
	}
	if template == "" {
		fmt.Fprintln(os.Stderr, "ERROR: argumen -o tidak ada")
		return 2
	}

	switch os.Getenv(modeEnv) {
	case "fail-once":
		marker := os.Getenv(markerEnv)
		if _, err := os.Stat(marker); err != nil {
			_ = os.WriteFile(marker, []byte("sudah gagal sekali"), 0o644)
			fmt.Fprintln(os.Stderr, "ERROR: Unable to download webpage: connection reset by peer")
			return 1
		}
	case "slow":
		fmt.Println("YTDLP_PROGRESS 1024 1048576 NA")
		time.Sleep(2 * time.Minute)
		return 0
	}

	audio, err := os.ReadFile(os.Getenv(audioEnv))
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return 1
	}
	total := len(audio)
	for _, done := range []int{0, total / 2, total} {
		fmt.Printf("YTDLP_PROGRESS %d %d NA\n", done, total)
	}
	target := strings.Replace(template, "%(ext)s", "wav", 1)
	if err := os.WriteFile(target, audio, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		return 1
	}
	return 0
}

// provider mengembalikan yt-dlp palsu dan FFmpeg sungguhan.
type provider struct {
	real *tools.Manager
	fake string
}

func (p provider) Resolve(ctx context.Context, name string) (string, string, error) {
	if name == tools.YTDLP {
		return p.fake, "2026.01.01", nil
	}
	return p.real.Resolve(ctx, name)
}

// Adapter di bawah mencerminkan cmd/app/adapters.go. Paket main tidak dapat
// diimpor, jadi bentuknya disalin; perbedaan di antara keduanya berarti
// test ini tidak lagi menguji wiring yang sama dengan aplikasi.

type downloader struct{ inner *ytdlp.Downloader }

func (a downloader) Download(
	ctx context.Context, req application.DownloadRequest, onProgress func(*float64),
) (*application.DownloadOutcome, error) {
	res, err := a.inner.Download(ctx, ytdlp.DownloadInput{
		SourceKey: req.SourceKey, TempDir: req.TempDir, Timeout: req.Timeout,
	}, func(p ytdlp.Progress) { onProgress(p.Percent()) })
	if err != nil {
		return nil, err
	}
	return &application.DownloadOutcome{AudioPath: res.AudioPath, CoverPath: res.ThumbnailPath}, nil
}

type transcoder struct{ inner *ffmpeg.Transcoder }

func (a transcoder) Transcode(
	ctx context.Context, req application.TranscodeRequest, onProgress func(*float64),
) error {
	return a.inner.Transcode(ctx, ffmpeg.TranscodeInput{
		AudioPath: req.AudioPath, CoverPath: req.CoverPath, OutputPath: req.OutputPath,
		Preset: req.Preset, Media: req.Media, Timeout: req.Timeout,
	}, func(p ffmpeg.Progress) { onProgress(p.Percent(req.Media.Duration)) })
}

type naming struct{}

func (naming) Build(mode domain.FilenameMode, info *domain.MediaInfo, ext string) string {
	return fs.BuildFilename(mode, info, ext)
}

type events struct {
	mu   sync.Mutex
	list []application.StreamEvent
}

func (e *events) Publish(ev application.StreamEvent) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.list = append(e.list, ev)
}

type stack struct {
	jobs    *db.JobRepository
	files   *db.FileRepository
	service *application.JobService
	sched   *worker.Scheduler
	outDir  string
	tmpDir  string
	ffprobe string
}

func newStack(t *testing.T, mode string) *stack {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	root := t.TempDir()

	manager, err := tools.New(filepath.Join(root, "tools"), filepath.Join(root, "tool-tmp"), log)
	if err != nil {
		t.Fatalf("tools.New() error = %v", err)
	}
	ffmpegBin, _, err := manager.Resolve(ctx, tools.FFmpeg)
	if err == nil {
		_, _, err = manager.Resolve(ctx, tools.FFprobe)
	}
	if err != nil {
		if os.Getenv("YT2MP3_REQUIRE_TOOLS") == "1" {
			t.Fatalf("ffmpeg/ffprobe wajib ada: %v", err)
		}
		t.Skipf("ffmpeg/ffprobe tidak ditemukan: %v", err)
	}
	ffprobeBin, _, _ := manager.Resolve(ctx, tools.FFprobe)

	audio := filepath.Join(root, "fixture.wav")
	if out, err := exec.Command(ffmpegBin, "-hide_banner", "-y", "-f", "lavfi",
		"-i", "sine=frequency=440:sample_rate=44100:duration=5", "-ac", "2",
		"-c:a", "pcm_s16le", audio).CombinedOutput(); err != nil {
		t.Fatalf("buat fixture: %v\n%s", err, out)
	}

	t.Setenv(roleEnv, "ytdlp")
	t.Setenv(modeEnv, mode)
	t.Setenv(audioEnv, audio)
	t.Setenv(markerEnv, filepath.Join(root, "marker"))

	database, err := db.Open(ctx, filepath.Join(root, "app.db"), log)
	if err != nil {
		t.Fatalf("buka database: %v", err)
	}
	if err := database.Migrate(ctx, "e2e"); err != nil {
		t.Fatalf("migrasi: %v", err)
	}

	s := &stack{
		jobs:    db.NewJobRepository(database),
		files:   db.NewFileRepository(database),
		outDir:  filepath.Join(root, "keluaran"),
		tmpDir:  filepath.Join(root, "tmp"),
		ffprobe: ffprobeBin,
	}

	store, err := fs.NewStore(s.outDir, s.tmpDir)
	if err != nil {
		t.Fatalf("siapkan store: %v", err)
	}
	presets := db.NewPresetRepository(database)
	media := db.NewMediaRepository(database)
	prov := provider{real: manager, fake: os.Args[0]}
	resolver := ytdlp.NewResolver(prov, log)
	pub := &events{}

	pipeline := application.NewPipeline(application.PipelineDeps{
		Repo: s.jobs, Closer: s.jobs, Presets: presets, Cache: media, Resolver: resolver,
		Downloader: downloader{inner: ytdlp.NewDownloader(prov, log)},
		Transcoder: transcoder{inner: ffmpeg.NewTranscoder(prov, log)},
		Verifier:   ffmpeg.NewProber(prov, log),
		Store:      store, Naming: naming{}, Events: pub, Log: log,
	})

	s.sched = worker.New(s.jobs, pipeline, pub, 2, log)
	s.sched.SetRetryPolicy(func(err *domain.Error, attempt int) (time.Duration, bool) {
		if attempt < application.MaxAutoRetries && err.Class == domain.ClassTransient {
			return 10 * time.Millisecond, true
		}
		return 0, false
	}, 0)

	live := func() application.LiveSettings {
		return application.LiveSettings{
			DefaultPresetID: "mp3_standard", FilenameMode: domain.FilenameTitle, MaxQueueDepth: 50,
		}
	}
	s.service = application.NewJobService(s.jobs, presets, media, resolver, s.files, pub,
		s.sched.Notify(), live, log)

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		s.sched.Run(runCtx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("scheduler tidak berhenti")
		}
		_ = database.Close()
	})
	return s
}

func (s *stack) create(t *testing.T) string {
	t.Helper()
	res, err := s.service.Create(context.Background(), application.CreateRequest{
		URL: testURL, PresetID: "mp3_standard",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return res.Job.ID
}

func (s *stack) wait(t *testing.T, id string, want domain.JobStatus) *domain.Job {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var last *domain.Job
	for time.Now().Before(deadline) {
		job, err := s.jobs.Get(context.Background(), id)
		if err == nil {
			last = job
			if job.Status == want {
				return job
			}
			if want != job.Status && job.Status.IsTerminal() {
				t.Fatalf("job berakhir %s (%s: %s), mau %s", job.Status, job.ErrorCode, job.ErrorMessage, want)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job tidak mencapai %s; terakhir %+v", want, last)
	return nil
}

// assertTempBersih memastikan pipeline tidak meninggalkan berkas sementara.
func (s *stack) assertTempBersih(t *testing.T, jobID string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(s.tmpDir, jobID)); !os.IsNotExist(err) {
		t.Errorf("direktori kerja job tertinggal: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(s.outDir, ".tmp")); len(entries) != 0 {
		t.Errorf("berkas commit sementara tertinggal: %d", len(entries))
	}
}

func TestE2EKonversiLengkap(t *testing.T) {
	s := newStack(t, "ok")
	id := s.create(t)
	job := s.wait(t, id, domain.StatusCompleted)

	// Job dibuat tanpa analisis lebih dulu, jadi judulnya berasal dari
	// metadata yang dibaca pipeline.
	if job.Title != testTitle {
		t.Errorf("judul = %q, mau %q", job.Title, testTitle)
	}

	file, err := s.files.GetByJob(context.Background(), id)
	if err != nil {
		t.Fatalf("berkas hasil tidak tercatat: %v", err)
	}
	if file.Filename != testTitle+".mp3" || filepath.Dir(file.Path) != s.outDir {
		t.Errorf("berkas = %s di %s", file.Filename, filepath.Dir(file.Path))
	}
	info, err := os.Stat(file.Path)
	if err != nil || info.Size() != file.SizeBytes {
		t.Fatalf("berkas di disk = %v, %v; tercatat %d byte", info, err, file.SizeBytes)
	}

	out, err := exec.Command(s.ffprobe, "-v", "error", "-print_format", "json",
		"-show_streams", "-show_format", file.Path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var probe struct {
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			SampleRate string `json:"sample_rate"`
		} `json:"streams"`
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("urai ffprobe: %v", err)
	}
	var audioOK bool
	for _, st := range probe.Streams {
		if st.CodecType == "audio" && st.CodecName == "mp3" && st.SampleRate == "48000" {
			audioOK = true
		}
	}
	if !audioOK {
		t.Errorf("stream hasil = %+v, mau mp3 48000 Hz", probe.Streams)
	}
	if got := probe.Format.Tags["title"]; got != testTitle {
		t.Errorf("tag title = %q", got)
	}

	s.assertTempBersih(t, id)
}

// Kegagalan jaringan sementara dari yt-dlp diulang otomatis dan job tetap
// selesai di baris yang sama.
func TestE2EAutoRetryKegagalanSementara(t *testing.T) {
	s := newStack(t, "fail-once")
	id := s.create(t)
	job := s.wait(t, id, domain.StatusCompleted)

	if job.AttemptCount != 1 {
		t.Errorf("attempt_count = %d, mau 1", job.AttemptCount)
	}
	s.assertTempBersih(t, id)
}

// Membatalkan saat yt-dlp mengunduh harus menghentikan prosesnya dan tidak
// meninggalkan berkas apa pun di folder keluaran.
func TestE2EBatalSaatMengunduh(t *testing.T) {
	s := newStack(t, "slow")
	id := s.create(t)
	s.wait(t, id, domain.StatusDownloading)

	start := time.Now()
	if err := s.sched.Cancel(context.Background(), id); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	s.wait(t, id, domain.StatusCancelled)
	if elapsed := time.Since(start); elapsed > 15*time.Second {
		t.Errorf("pembatalan butuh %s; proses yt-dlp kemungkinan tidak dihentikan", elapsed)
	}

	entries, _ := os.ReadDir(s.outDir)
	for _, e := range entries {
		if !e.IsDir() {
			t.Errorf("berkas tertinggal di folder keluaran: %s", e.Name())
		}
	}
	s.assertTempBersih(t, id)
}

// Judul dan artis suntingan menentukan tag sekaligus nama berkas, sementara
// cache metadata tetap menyimpan judul aslinya.
func TestE2ETagSuntingan(t *testing.T) {
	s := newStack(t, "ok")
	res, err := s.service.Create(context.Background(), application.CreateRequest{
		URL: testURL, PresetID: "mp3_standard", Title: "Lagu Suntingan", Artist: "Artis Suntingan",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	s.wait(t, res.Job.ID, domain.StatusCompleted)

	file, err := s.files.GetByJob(context.Background(), res.Job.ID)
	if err != nil {
		t.Fatalf("berkas hasil tidak tercatat: %v", err)
	}
	if file.Filename != "Lagu Suntingan.mp3" {
		t.Errorf("nama berkas = %q", file.Filename)
	}

	out, err := exec.Command(s.ffprobe, "-v", "error", "-print_format", "json",
		"-show_format", file.Path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var probe struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		t.Fatalf("urai ffprobe: %v", err)
	}
	if probe.Format.Tags["title"] != "Lagu Suntingan" || probe.Format.Tags["artist"] != "Artis Suntingan" {
		t.Errorf("tag = %v", probe.Format.Tags)
	}
}

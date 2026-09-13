package ytdlp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
)

// stem adalah nama dasar berkas unduhan di dalam direktori kerja job.
const stem = "source"

// Downloader mengambil audio sumber lewat yt-dlp.
type Downloader struct {
	tools ToolProvider
	log   *slog.Logger
}

// NewDownloader membuat downloader.
func NewDownloader(tp ToolProvider, log *slog.Logger) *Downloader {
	return &Downloader{tools: tp, log: log}
}

// DownloadInput adalah parameter satu unduhan.
type DownloadInput struct {
	SourceKey string
	TempDir   string
	Timeout   time.Duration
}

// DownloadResult menunjuk berkas hasil unduhan.
type DownloadResult struct {
	AudioPath string

	// ThumbnailPath kosong bila sampul gagal diambil. Itu bukan kegagalan:
	// berkas tanpa sampul tetap keluaran yang sah.
	ThumbnailPath string
}

// downloadArgs menyusun argv unduhan.
//
// Flag hardening sama dengan jalur metadata: tanpa --ignore-config, berkas
// yt-dlp.conf milik pengguna dapat menyuntikkan --exec.
func downloadArgs(url, tempDir, ffmpegPath string) []string {
	return []string{
		"--ignore-config",
		"--no-exec",
		"--no-playlist",
		"--no-warnings",
		"--newline",
		"--progress-template", progressTemplate,

		// yt-dlp mencari FFmpeg sendiri, dan hasilnya tidak selalu sama dengan
		// FFmpeg yang ditemukan aplikasi. Terbukti pada aplikasi yang dibuka
		// dari installer: FFmpeg ada di PATH dan aplikasi melaporkan tool
		// siap, tetapi yt-dlp terkelola gagal dengan "ffmpeg not found" dan
		// setiap unduhan gagal. Menunjuk langsung ke FFmpeg yang sama
		// menyamakan keduanya.
		"--ffmpeg-location", ffmpegPath,

		// Hanya trek audio yang diunduh. Tanpa ini yt-dlp mengambil stream
		// video lengkap lalu membuangnya, sepuluh kali lipat bandwidth
		// untuk hasil yang sama. Lihat ADR-015.
		"-f", "bestaudio/best",

		"--write-thumbnail",
		"--convert-thumbnail", "jpg",
		"--retries", "2",
		"--socket-timeout", "30",
		"-o", filepath.Join(tempDir, stem+".%(ext)s"),
		"--", // akhiri parsing flag sebelum URL
		url,
	}
}

// Download menjalankan yt-dlp dan melaporkan kemajuannya.
func (d *Downloader) Download(
	ctx context.Context, in DownloadInput, onProgress func(Progress),
) (*DownloadResult, error) {
	bin, _, err := d.tools.Resolve(ctx, tools.YTDLP)
	if err != nil {
		return nil, err
	}
	// Konversi tidak mungkin berhasil tanpa FFmpeg, jadi ketiadaannya
	// dilaporkan sebelum satu byte pun diunduh.
	ffmpegBin, _, err := d.tools.Resolve(ctx, tools.FFmpeg)
	if err != nil {
		return nil, err
	}

	runCtx := ctx
	if in.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, in.Timeout)
		defer cancel()
	}

	h, err := process.Start(runCtx, process.Spec{
		Bin:  bin,
		Args: downloadArgs(CanonicalURL(in.SourceKey), in.TempDir, ffmpegBin),
	})
	if err != nil {
		return nil, domain.WrapError(domain.CodeDownloadFailed, domain.ClassTransient,
			"jalankan yt-dlp", err)
	}

	// Kedua pipa wajib dikuras sampai habis: proses yang menulis ke pipa
	// penuh akan menggantung selamanya.
	var wg sync.WaitGroup
	var stderrBuf strings.Builder

	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = ParseProgress(h.Stdout, onProgress, func(line string) {
			d.log.Debug("yt-dlp", "line", line)
		})
	}()
	go func() {
		defer wg.Done()
		_ = ParseProgress(h.Stderr, onProgress, func(line string) {
			if stderrBuf.Len() < 64<<10 {
				stderrBuf.WriteString(line)
				stderrBuf.WriteString("\n")
			}
		})
	}()

	wg.Wait()
	waitErr := h.Wait()

	if h.ExitCode() != 0 || waitErr != nil {
		if runCtx.Err() != nil && ctx.Err() == nil {
			return nil, domain.NewError(domain.CodeTimeout, domain.ClassTransient,
				"unduhan melewati batas waktu")
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, mapStderr([]byte(stderrBuf.String()), h.ExitCode())
	}

	return collectOutputs(in.TempDir)
}

// collectOutputs menemukan berkas hasil unduhan.
//
// Ekstensi audio tidak dapat ditebak lebih dulu karena bergantung pada
// format yang dipilih yt-dlp (webm untuk Opus, m4a untuk AAC), jadi
// direktori kerja dipindai.
func collectOutputs(tempDir string) (*DownloadResult, error) {
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		return nil, domain.WrapError(domain.CodeDownloadFailed, domain.ClassLocal,
			"baca direktori unduhan", err)
	}

	var result DownloadResult
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, stem+".") {
			continue
		}

		path := filepath.Join(tempDir, name)
		switch strings.ToLower(filepath.Ext(name)) {
		case ".jpg", ".jpeg", ".png", ".webp":
			result.ThumbnailPath = path
		case ".part", ".ytdl":
			// sisa unduhan yang belum selesai, abaikan
		default:
			result.AudioPath = path
		}
	}

	if result.AudioPath == "" {
		return nil, domain.NewError(domain.CodeDownloadFailed, domain.ClassTransient,
			fmt.Sprintf("berkas audio tidak ditemukan di %s", tempDir))
	}
	return &result, nil
}

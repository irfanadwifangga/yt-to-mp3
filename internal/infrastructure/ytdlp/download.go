package ytdlp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
)

// stem adalah nama dasar berkas unduhan di dalam direktori kerja job.
const stem = "source"

// Downloader mengambil media sumber lewat yt-dlp.
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
	Selection Selection
}

// Selection menentukan stream yang diunduh.
type Selection struct {
	// Video meminta stream video beserta audionya; tanpa itu hanya audio
	// yang diunduh.
	Video bool

	// MaxHeight membatasi resolusi video dalam satuan label "p" YouTube,
	// yaitu sisi terpendek bingkai. Nol berarti tertinggi yang tersedia.
	MaxHeight int
}

// DownloadResult menunjuk berkas hasil unduhan.
type DownloadResult struct {
	// MediaPath adalah audio untuk unduhan audio, atau video beserta
	// audionya untuk unduhan video.
	MediaPath string

	// ThumbnailPath kosong bila sampul gagal diambil atau tidak diminta. Itu
	// bukan kegagalan: berkas tanpa sampul tetap keluaran yang sah.
	ThumbnailPath string
}

// formatArgs memilih stream yang diunduh.
func formatArgs(sel Selection) []string {
	if !sel.Video {
		return []string{
			// Hanya trek audio yang diunduh. Tanpa ini yt-dlp mengambil
			// stream video lengkap lalu membuangnya, sepuluh kali lipat
			// bandwidth untuk hasil yang sama. Lihat ADR-015.
			"-f", "bestaudio/best",

			"--write-thumbnail",
			"--convert-thumbnail", "jpg",
		}
	}

	// Urutan kriteria: resolusi lebih dulu, supaya pilihan 720p memang
	// menghasilkan 720p, lalu H.264 dan AAC. Keduanya codec yang dapat
	// disalin apa adanya ke MP4 yang diputar di mana saja; sumber tanpa
	// H.264 pada resolusi itu tetap diunduh lalu di-encode ulang oleh
	// transcoder. Lihat planning §12.1.
	res := "res"
	if sel.MaxHeight > 0 {
		// res:N berarti setinggi mungkin tetapi tidak melebihi N, atau yang
		// terkecil bila sumber tidak punya resolusi serendah itu.
		res = "res:" + strconv.Itoa(sel.MaxHeight)
	}
	return []string{
		"-f", "bv*+ba/b",
		"-S", res + ",vcodec:h264,acodec:aac",

		// MKV menampung codec apa pun, jadi penggabungan video dan audio
		// oleh yt-dlp tidak pernah gagal karena kombinasi codec. Wadah MP4
		// akhirnya disusun transcoder.
		"--merge-output-format", "mkv",
	}
}

// downloadArgs menyusun argv unduhan.
//
// Flag hardening sama dengan jalur metadata: tanpa --ignore-config, berkas
// yt-dlp.conf milik pengguna dapat menyuntikkan --exec.
func downloadArgs(url, tempDir, ffmpegPath string, sel Selection) []string {
	args := []string{
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
	}
	args = append(args, formatArgs(sel)...)
	return append(args,
		"--retries", "2",
		"--socket-timeout", "30",
		"-o", filepath.Join(tempDir, stem+".%(ext)s"),
		"--", // akhiri parsing flag sebelum URL
		url,
	)
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
		Args: downloadArgs(CanonicalURL(in.SourceKey), in.TempDir, ffmpegBin, in.Selection),
	})
	if err != nil {
		return nil, domain.WrapError(domain.CodeDownloadFailed, domain.ClassTransient,
			"jalankan yt-dlp", err)
	}

	// Unduhan video terdiri dari beberapa berkas yang dilaporkan dari dua
	// pipa sekaligus, jadi penggabung progresnya dijaga mutex.
	if in.Selection.Video {
		var mu sync.Mutex
		var parts partTracker
		report := onProgress
		onProgress = func(p Progress) {
			mu.Lock()
			p = parts.place(p)
			mu.Unlock()
			report(p)
		}
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
// Ekstensi tidak dapat ditebak lebih dulu karena bergantung pada format
// yang dipilih yt-dlp (webm untuk Opus, m4a untuk AAC, mkv untuk video yang
// digabung), jadi direktori kerja dipindai.
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
			// Bagian video dan audio sebelum digabung bernama
			// source.f137.mp4; yang dipakai hanya hasil gabungannya.
			if strings.Contains(strings.TrimPrefix(name, stem+"."), ".") {
				continue
			}
			result.MediaPath = path
		}
	}

	if result.MediaPath == "" {
		return nil, domain.NewError(domain.CodeDownloadFailed, domain.ClassTransient,
			fmt.Sprintf("berkas media tidak ditemukan di %s", tempDir))
	}
	return &result, nil
}

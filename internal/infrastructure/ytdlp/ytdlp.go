// Package ytdlp membungkus yt-dlp: pembangunan argv, parsing keluaran, dan
// pemetaan error ke kode domain.
package ytdlp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
)

// metadataTimeout membatasi pemanggilan metadata. Resolving tidak mengunduh
// media, jadi batas ini pendek dan tetap.
const metadataTimeout = 60 * time.Second

// ToolProvider menyediakan path binary. Interface didefinisikan di sisi
// pemakai supaya paket ini tidak bergantung pada implementasi tertentu.
type ToolProvider interface {
	Resolve(ctx context.Context, name string) (path string, version string, err error)
}

// Resolver mengambil metadata sumber lewat yt-dlp.
type Resolver struct {
	tools ToolProvider
	log   *slog.Logger
}

// NewResolver membuat Resolver.
func NewResolver(tp ToolProvider, log *slog.Logger) *Resolver {
	return &Resolver{tools: tp, log: log}
}

// CanonicalURL menyusun ulang URL dari source key.
//
// yt-dlp selalu menerima bentuk kanonik ini, bukan URL mentah dari pengguna:
// parameter pelacakan dan playlist tidak pernah ikut, dan hasilnya
// deterministik untuk video yang sama.
func CanonicalURL(sourceKey string) string {
	return "https://www.youtube.com/watch?v=" + domain.VideoID(sourceKey)
}

// metadataArgs menyusun argv untuk pengambilan metadata.
func metadataArgs(url string) []string {
	return []string{
		// Abaikan yt-dlp.conf milik pengguna: berkas itu bisa menyuntikkan
		// --exec dan menjadikannya jalur eksekusi perintah sewenang-wenang.
		"--ignore-config",
		"--no-exec",
		"--no-playlist",
		"--no-warnings",
		"--dump-single-json",
		"--skip-download",
		"--socket-timeout", "30",
		"--retries", "2",
		"--", // akhiri parsing flag sebelum URL
		url,
	}
}

// rawMetadata adalah subset keluaran JSON yt-dlp yang kita pakai.
type rawMetadata struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Uploader  string  `json:"uploader"`
	Channel   string  `json:"channel"`
	Duration  float64 `json:"duration"`
	Thumbnail string  `json:"thumbnail"`
	ACodec    string  `json:"acodec"`
	ASR       int     `json:"asr"`
	IsLive    bool    `json:"is_live"`
	LiveNow   bool    `json:"live_status_is_live"`
	LiveState string  `json:"live_status"`

	// Data katalog YouTube Music; kosong untuk unggahan biasa. yt-dlp
	// mengirim null untuk release_year yang tidak diketahui, yang terurai
	// menjadi 0.
	Track       string   `json:"track"`
	Artist      string   `json:"artist"`
	Artists     []string `json:"artists"`
	Album       string   `json:"album"`
	ReleaseYear int      `json:"release_year"`

	// Formats hanya dibaca untuk resolusi video tertinggi yang tersedia.
	Formats []rawFormat `json:"formats"`
}

// rawFormat adalah subset satu entri formats keluaran yt-dlp.
type rawFormat struct {
	VCodec string `json:"vcodec"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// maxVideoHeight mencari resolusi tertinggi di antara format bervideo,
// dalam satuan label "p" YouTube.
//
// Labelnya adalah sisi terpendek bingkai, sama dengan cara yt-dlp
// mengurutkan res: video vertikal 1080×1920 adalah 1080p, bukan 1920p.
// Format tanpa video (audio saja dan storyboard) bervcodec "none" namun
// storyboard tetap punya ukuran, jadi keduanya wajib disaring.
func maxVideoHeight(formats []rawFormat) int {
	highest := 0
	for _, f := range formats {
		if f.VCodec == "" || f.VCodec == "none" {
			continue
		}
		side := f.Height
		if f.Width > 0 && f.Width < side {
			side = f.Width
		}
		highest = max(highest, side)
	}
	return highest
}

// Resolve mengambil metadata untuk satu source key.
func (r *Resolver) Resolve(ctx context.Context, sourceKey string) (*domain.MediaInfo, error) {
	bin, _, err := r.tools.Resolve(ctx, tools.YTDLP)
	if err != nil {
		return nil, err
	}

	url := CanonicalURL(sourceKey)
	res, err := process.Output(ctx, process.Spec{
		Bin:     bin,
		Args:    metadataArgs(url),
		Timeout: metadataTimeout,
	})
	if err != nil {
		return nil, domain.WrapError(domain.CodeTimeout, domain.ClassTransient,
			"pemanggilan metadata gagal", err)
	}
	if res.ExitCode != 0 {
		return nil, mapStderr(res.Stderr, res.ExitCode)
	}

	var raw rawMetadata
	if err := json.Unmarshal(res.Stdout, &raw); err != nil {
		return nil, domain.WrapError(domain.CodeInternal, domain.ClassTransient,
			"keluaran metadata bukan JSON yang dikenal", err)
	}

	// Livestream tidak berdurasi dan tidak pernah selesai; ditolak sebelum
	// ada satu byte pun yang diunduh. Lihat ADR-026.
	if raw.IsLive || raw.LiveNow || raw.LiveState == "is_live" {
		return nil, domain.NewError(domain.CodeLiveNotSupported, domain.ClassPermanent,
			"sumber adalah siaran langsung")
	}

	return mediaFromRaw(sourceKey, url, raw), nil
}

// mediaFromRaw menormalkan keluaran JSON yt-dlp menjadi MediaInfo.
func mediaFromRaw(sourceKey, url string, raw rawMetadata) *domain.MediaInfo {
	// "artists" adalah daftar resmi; "artist" lama dipakai bila daftar itu
	// tidak ada.
	artist := strings.Join(raw.Artists, ", ")
	if artist == "" {
		artist = raw.Artist
	}
	// Tahun di luar rentang wajar dibuang: lebih baik tanpa tag tahun
	// daripada tahun yang jelas salah.
	year := raw.ReleaseYear
	if year < 1900 || year > 2100 {
		year = 0
	}

	return &domain.MediaInfo{
		SourceKey:    sourceKey,
		SourceURL:    url,
		Title:        raw.Title,
		Uploader:     firstNonEmpty(raw.Uploader, raw.Channel),
		Duration:     durationOf(raw.Duration),
		DurationMS:   int64(durationOf(raw.Duration) / time.Millisecond),
		ThumbnailURL: raw.Thumbnail,
		SourceCodec:  raw.ACodec,
		SampleRate:   raw.ASR,
		Track:        raw.Track,
		Artist:       artist,
		Album:        raw.Album,
		ReleaseYear:  year,
		VideoHeight:  maxVideoHeight(raw.Formats),
	}
}

// durationOf mengubah detik pecahan jadi Duration, menolak nilai tak wajar.
func durationOf(seconds float64) time.Duration {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0 // durasi tidak diketahui; bukan nilai hilang, tapi memang nol
	}
	return time.Duration(seconds * float64(time.Second))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// Version mengembalikan versi yt-dlp yang terpasang.
func (r *Resolver) Version(ctx context.Context) (string, error) {
	_, version, err := r.tools.Resolve(ctx, tools.YTDLP)
	if err != nil {
		return "", fmt.Errorf("resolve yt-dlp: %w", err)
	}
	return version, nil
}

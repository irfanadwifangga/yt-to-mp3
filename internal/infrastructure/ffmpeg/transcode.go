package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
)

// ToolProvider menyediakan path binary.
type ToolProvider interface {
	Resolve(ctx context.Context, name string) (path string, version string, err error)
}

// Transcoder mengubah media sumber menjadi format keluaran.
type Transcoder struct {
	tools ToolProvider
	log   *slog.Logger
}

// NewTranscoder membuat transcoder.
func NewTranscoder(tp ToolProvider, log *slog.Logger) *Transcoder {
	return &Transcoder{tools: tp, log: log}
}

// TranscodeInput adalah parameter satu konversi.
type TranscodeInput struct {
	// MediaPath adalah berkas hasil unduhan: audio saja untuk preset audio,
	// video beserta audionya untuk preset video.
	MediaPath string

	// CoverPath opsional dan hanya dipakai preset audio. Kegagalan pada
	// jalur sampul tidak pernah menggagalkan job; berkas tanpa sampul tetap
	// keluaran yang sah.
	CoverPath string

	OutputPath string
	Preset     *domain.Preset
	Media      *domain.MediaInfo
	Timeout    time.Duration

	// CopyVideo dan CopyAudio menyalin stream apa adanya alih-alih
	// meng-encode ulang. Transcode mengisinya dari hasil probe berkas sumber
	// untuk preset yang mengizinkan passthrough; lihat copyPlan.
	CopyVideo bool
	CopyAudio bool
}

// Parameter encode ulang H.264, untuk MP4, MOV, dan FLV. H.264 8-bit 4:2:0
// adalah satu-satunya kombinasi yang pasti diputar pemutar bawaan Windows,
// TV, dan ponsel lama. veryfast menjaga konversi 1080p tetap mendekati waktu
// nyata di CPU biasa; CRF 20 nyaris tak terbedakan dari sumber pada
// kecepatan itu.
const (
	videoEncoder = "libx264"
	videoPreset  = "veryfast"
	videoCRF     = "20"
	videoPixFmt  = "yuv420p"
)

// videoEncodeArgs memilih encoder video untuk wadah yang sumbernya tidak
// dapat disalin. Lihat ADR-036.
func videoEncodeArgs(format string) []string {
	switch format {
	case "webm":
		// VP9 mode realtime: encoder VP9 bawaan sangat lambat, dan WebM
		// hanya di-encode bila YouTube tidak menyajikan VP9 pada resolusi
		// itu, yang jarang terjadi.
		return []string{
			"-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "8",
			"-row-mt", "1", "-crf", "32", "-b:v", "0", "-pix_fmt", videoPixFmt,
		}
	case "avi":
		// MPEG-4 Part 2 dengan FourCC XVID, yang dikenali pemutar DVD dan
		// perangkat lama yang menjadi alasan orang masih meminta AVI.
		return []string{"-c:v", "mpeg4", "-vtag", "xvid", "-q:v", "3", "-pix_fmt", videoPixFmt}
	default:
		return []string{
			"-c:v", videoEncoder, "-preset", videoPreset, "-crf", videoCRF, "-pix_fmt", videoPixFmt,
		}
	}
}

// copyPlan memutuskan stream mana yang disalin apa adanya.
//
// Hanya preset dengan passthrough yang menyalin, dan hanya codec yang
// memang ditampung wadahnya. H.264 selain 8-bit 4:2:0 sah di MP4 tetapi
// ditolak banyak pemutar perangkat keras, jadi tetap di-encode ulang.
func copyPlan(p *domain.Preset, f domain.OutputFormat, src *ProbeResult) (video, audio bool) {
	if !p.Passthrough {
		return false, false
	}
	video = src.HasVideo && domain.Copies(f.VideoCopy, src.VideoCodec)
	if video && src.VideoCodec == "h264" && f.PreferVideo == "h264" {
		video = src.PixFmt == "yuv420p" || src.PixFmt == "yuvj420p"
	}
	audio = src.HasAudio && domain.Copies(f.AudioCopy, src.Codec)
	return video, audio
}

// maxCoverSide membatasi sisi sampul supaya setiap berkas tidak membawa
// gambar berukuran megabyte. 800 piksel masih tajam di layar ponsel.
const maxCoverSide = 800

// coverFilter memotong thumbnail ke persegi di tengah lalu mengecilkannya.
//
// Thumbnail YouTube berbentuk 16:9, sedangkan pemutar musik menampilkan
// sampul sebagai persegi: tanpa pemotongan, sampul tampil gepeng atau
// dipotong sembarang oleh pemutarnya. Untuk unggahan musik, artwork persegi
// hampir selalu berada di tengah bingkai. Koma dalam ekspresi di-escape
// karena koma memisahkan filter pada filtergraph.
var coverFilter = fmt.Sprintf(
	`crop=min(iw\,ih):min(iw\,ih),scale=min(iw\,%[1]d):min(ih\,%[1]d)`, maxCoverSide)

// BuildArgs menyusun argv FFmpeg untuk satu konversi.
//
// Dipisah sebagai fungsi murni supaya semantik preset dapat diuji tanpa
// menjalankan FFmpeg sama sekali.
func BuildArgs(in TranscodeInput) []string {
	format, _ := domain.FormatOf(in.Preset.Format)

	args := []string{
		"-hide_banner",
		"-nostdin",
		"-y",
		"-i", in.MediaPath,
	}

	withCover := !in.Preset.IsVideo() && format.Cover && in.CoverPath != ""
	switch {
	case in.Preset.IsVideo():
		args = append(args, "-map", "0:v:0", "-map", "0:a:0")
	case withCover:
		args = append(args, "-i", in.CoverPath)
		args = append(args, "-map", "0:a:0", "-map", "1:v:0")
	default:
		args = append(args, "-map", "0:a:0")
	}

	if in.Preset.IsVideo() {
		if in.CopyVideo {
			args = append(args, "-c:v", "copy")
		} else {
			args = append(args, videoEncodeArgs(in.Preset.Format)...)
		}
	}

	if in.CopyAudio {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, audioEncodeArgs(in.Preset)...)
	}

	if withCover {
		args = append(args,
			"-filter:v:0", coverFilter,
			"-c:v", "mjpeg",
			// Tanpa -q:v, mjpeg memakai bitrate bawaan yang membuat sampul
			// tampak pecah di layar besar.
			"-q:v", "2",
			"-disposition:v:0", "attached_pic",
		)
	}

	if in.Preset.Format == "mp3" {
		// v2.3 lebih luas didukung pemutar daripada v2.4, termasuk Windows
		// Explorer. Lihat ADR-020.
		args = append(args, "-id3v2_version", "3")
	}
	if format.FastStart {
		// Indeks moov dipindah ke awal berkas supaya berkas dapat mulai
		// diputar sebelum seluruhnya terbaca, termasuk dari folder jaringan.
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, metadataArgs(in.Media)...)

	args = append(args, "-progress", "pipe:1", "-nostats")
	args = append(args, in.OutputPath)
	return args
}

// audioEncodeArgs menyusun encoder audio dari preset.
func audioEncodeArgs(p *domain.Preset) []string {
	args := []string{"-c:a", p.Codec}
	args = append(args, qualityArgs(p)...)

	// Lossless dari Opus yang di-decode ke float: tanpa -sample_fmt, FLAC dan
	// ALAC memilih 32-bit yang dua kali lebih besar tanpa informasi tambahan,
	// karena sumbernya sendiri lossy.
	switch p.Codec {
	case "flac":
		args = append(args, "-sample_fmt", "s16")
	case "alac":
		args = append(args, "-sample_fmt", "s16p")
	}

	// sample_rate kosong berarti ikut sumber: preset lossless kehilangan
	// maknanya bila di-resample, dan audio yang menyertai video tidak perlu.
	// Lihat ADR-030.
	if p.SampleRate != nil {
		args = append(args, "-ar", strconv.Itoa(*p.SampleRate))
	}
	if p.Channels > 0 {
		args = append(args, "-ac", strconv.Itoa(p.Channels))
	}
	return args
}

// qualityArgs memilih antara bitrate tetap dan kualitas variabel. Preset
// lossless tidak punya keduanya.
func qualityArgs(p *domain.Preset) []string {
	if p.Mode == domain.ModeVBR && p.VBRQuality != nil {
		return []string{"-q:a", strconv.Itoa(*p.VBRQuality)}
	}
	if p.BitrateKbps != nil {
		return []string{"-b:a", strconv.Itoa(*p.BitrateKbps) + "k"}
	}
	return nil
}

// metadataArgs menyusun tag ID3 dari metadata sumber.
//
// Field kosong dilewati, bukan ditulis kosong: tag kosong tampil sebagai
// entri bernilai hampa di pemutar, lebih buruk daripada tidak ada tag.
func metadataArgs(m *domain.MediaInfo) []string {
	if m == nil {
		return nil
	}

	var args []string
	add := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			args = append(args, "-metadata", key+"="+value)
		}
	}

	add("title", m.Title)
	// Artis dari katalog musik lebih tepat daripada nama kanal; nama kanal
	// hanya cadangan.
	add("artist", firstNonEmpty(m.Artist, m.Uploader))
	// Album dan tahun hanya ditulis dari data rilis. Nama kanal sebagai album
	// mengelompokkan lagu-lagu yang tidak berkaitan menjadi satu album di
	// pemutar, dan tahun unggah bukan tahun rilis lagu.
	add("album", m.Album)
	if m.ReleaseYear > 0 {
		add("date", strconv.Itoa(m.ReleaseYear))
	}
	add("comment", m.SourceURL)
	return args
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Transcode menjalankan FFmpeg dan melaporkan kemajuannya.
//
// Bila konversi dengan sampul gagal, konversi diulang sekali tanpa sampul.
// Thumbnail yang rusak atau berformat aneh tidak boleh menggagalkan job:
// berkas tanpa sampul tetap keluaran yang sah.
func (t *Transcoder) Transcode(
	ctx context.Context, in TranscodeInput, onProgress func(Progress),
) error {
	bin, _, err := t.tools.Resolve(ctx, tools.FFmpeg)
	if err != nil {
		return err
	}

	format, ok := domain.FormatOf(in.Preset.Format)
	if !ok {
		return domain.NewError(domain.CodeInternal, domain.ClassPermanent,
			fmt.Sprintf("format %s tidak dikenal", in.Preset.Format))
	}
	if !format.Cover {
		in.CoverPath = ""
	}

	// Sumber hanya perlu diperiksa bila ada yang mungkin disalin, atau bila
	// hasilnya wajib memuat video.
	if in.Preset.IsVideo() || in.Preset.Passthrough {
		src, err := NewProber(t.tools, t.log).Probe(ctx, in.MediaPath)
		if err != nil {
			// Pembatalan dan ffprobe yang hilang dilaporkan apa adanya;
			// hanya kegagalan membaca berkasnya yang menjadi kegagalan
			// konversi, bukan kegagalan verifikasi hasil.
			var derr *domain.Error
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.As(err, &derr) && derr.Code != domain.CodeVerifyFailed {
				return err
			}
			return domain.WrapError(domain.CodeTranscodeFailed, domain.ClassTransient,
				"periksa berkas sumber", err)
		}
		if !src.HasAudio || (in.Preset.IsVideo() && !src.HasVideo) {
			return domain.NewError(domain.CodeTranscodeFailed, domain.ClassTransient,
				"berkas sumber tidak memuat stream yang dibutuhkan")
		}
		in.CopyVideo, in.CopyAudio = copyPlan(in.Preset, format, src)
		t.log.Info("rencana konversi", "format", in.Preset.Format,
			"video", src.VideoCodec, "pix_fmt", src.PixFmt, "salin_video", in.CopyVideo,
			"audio", src.Codec, "salin_audio", in.CopyAudio)
	}

	err = t.run(ctx, bin, in, onProgress)
	var derr *domain.Error
	if err == nil || in.CoverPath == "" || ctx.Err() != nil ||
		!errors.As(err, &derr) || derr.Code != domain.CodeTranscodeFailed {
		return err
	}

	t.log.Warn("konversi dengan sampul gagal, diulang tanpa sampul", "error", err)
	withoutCover := in
	withoutCover.CoverPath = ""
	return t.run(ctx, bin, withoutCover, onProgress)
}

// run menjalankan satu proses FFmpeg sampai selesai.
func (t *Transcoder) run(
	ctx context.Context, bin string, in TranscodeInput, onProgress func(Progress),
) error {
	runCtx := ctx
	if in.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, in.Timeout)
		defer cancel()
	}

	h, err := process.Start(runCtx, process.Spec{Bin: bin, Args: BuildArgs(in)})
	if err != nil {
		return domain.WrapError(domain.CodeTranscodeFailed, domain.ClassTransient,
			"jalankan ffmpeg", err)
	}

	// Kedua pipa wajib dikuras: FFmpeg cerewet di stderr, dan pipa penuh
	// akan menggantungkan prosesnya.
	var wg sync.WaitGroup
	var stderrBuf strings.Builder

	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = ParseProgress(h.Stdout, onProgress)
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := h.Stderr.Read(buf)
			if n > 0 && stderrBuf.Len() < 64<<10 {
				stderrBuf.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	wg.Wait()
	waitErr := h.Wait()

	if h.ExitCode() != 0 || waitErr != nil {
		if runCtx.Err() != nil && ctx.Err() == nil {
			return domain.NewError(domain.CodeTimeout, domain.ClassTransient,
				"konversi melewati batas waktu")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return domain.NewError(domain.CodeTranscodeFailed, domain.ClassTransient,
			fmt.Sprintf("ffmpeg keluar dengan kode %d: %s",
				h.ExitCode(), truncate(stderrBuf.String())))
	}
	return nil
}

// truncate memangkas detail agar log tidak dibanjiri keluaran panjang.
func truncate(s string) string {
	const limit = 500
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[len(s)-limit:] // ekor lebih informatif: di situ pesan errornya
}

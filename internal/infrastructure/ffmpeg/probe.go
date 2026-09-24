package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
)

// probeTimeout cukup panjang untuk berkas besar, tetapi tetap berbatas.
const probeTimeout = 60 * time.Second

// Prober memeriksa berkas hasil dengan ffprobe.
type Prober struct {
	tools ToolProvider
	log   *slog.Logger
}

// NewProber membuat prober.
func NewProber(tp ToolProvider, log *slog.Logger) *Prober {
	return &Prober{tools: tp, log: log}
}

// ProbeResult adalah ringkasan berkas media.
type ProbeResult struct {
	Duration   time.Duration
	HasAudio   bool
	Codec      string
	SampleRate int

	// HasVideo hanya menghitung stream video sungguhan. Sampul yang
	// disematkan di MP3 juga stream video, tetapi bukan video.
	HasVideo   bool
	VideoCodec string
	PixFmt     string
}

// MP4ReadyVideo melaporkan apakah stream video dapat disalin apa adanya ke
// MP4 yang diputar di mana saja: H.264 8-bit 4:2:0.
func (r *ProbeResult) MP4ReadyVideo() bool {
	return r.VideoCodec == "h264" && (r.PixFmt == "yuv420p" || r.PixFmt == "yuvj420p")
}

// MP4ReadyAudio melaporkan apakah stream audio dapat disalin apa adanya ke
// MP4. Opus di dalam MP4 sah menurut spesifikasi, tetapi banyak pemutar
// bawaan menolaknya.
func (r *ProbeResult) MP4ReadyAudio() bool {
	return r.Codec == "aac"
}

// probeOutput memetakan keluaran JSON ffprobe.
type probeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		SampleRate  string `json:"sample_rate"`
		PixFmt      string `json:"pix_fmt"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
}

// Probe membaca properti berkas.
func (p *Prober) Probe(ctx context.Context, path string) (*ProbeResult, error) {
	bin, _, err := p.tools.Resolve(ctx, tools.FFprobe)
	if err != nil {
		return nil, err
	}

	res, err := process.Output(ctx, process.Spec{
		Bin: bin,
		Args: []string{
			"-v", "error",
			"-show_entries", "format=duration:stream=codec_type,codec_name,sample_rate,pix_fmt" +
				":stream_disposition=attached_pic",
			"-of", "json",
			"--", path,
		},
		Timeout: probeTimeout,
	})
	if err != nil {
		return nil, domain.WrapError(domain.CodeVerifyFailed, domain.ClassLocal,
			"jalankan ffprobe", err)
	}
	if res.ExitCode != 0 {
		return nil, domain.NewError(domain.CodeVerifyFailed, domain.ClassLocal,
			fmt.Sprintf("ffprobe keluar dengan kode %d", res.ExitCode))
	}

	var out probeOutput
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return nil, domain.WrapError(domain.CodeVerifyFailed, domain.ClassLocal,
			"keluaran ffprobe bukan JSON yang dikenal", err)
	}

	// Hanya stream pertama tiap jenis yang dibaca, sama dengan stream yang
	// dipetakan transcoder lewat 0:a:0 dan 0:v:0.
	result := &ProbeResult{Duration: parseSeconds(out.Format.Duration)}
	for _, s := range out.Streams {
		switch {
		case s.CodecType == "audio" && !result.HasAudio:
			result.HasAudio = true
			result.Codec = s.CodecName
			if rate, err := strconv.Atoi(s.SampleRate); err == nil {
				result.SampleRate = rate
			}
		case s.CodecType == "video" && s.Disposition.AttachedPic == 0 && !result.HasVideo:
			result.HasVideo = true
			result.VideoCodec = s.CodecName
			result.PixFmt = s.PixFmt
		}
	}
	return result, nil
}

// parseSeconds mengubah detik pecahan jadi Duration, menolak nilai tak wajar.
func parseSeconds(s string) time.Duration {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return time.Duration(v * float64(time.Second))
}

// DurationTolerance adalah selisih durasi yang masih dianggap wajar.
//
// Encoder MP3 menambahkan padding frame di awal dan akhir, sehingga hasil
// selalu sedikit lebih panjang dari sumber. Toleransi ini membedakan padding
// normal dari konversi yang benar-benar terpotong.
const DurationTolerance = 2 * time.Second

// Verify memastikan berkas hasil layak dianggap sukses.
//
// Berkas video wajib memuat stream video selain audio: MP4 yang hanya
// berisi suara bukan hasil yang diminta pengguna.
func (p *Prober) Verify(ctx context.Context, path string, expected time.Duration, kind domain.PresetKind) error {
	result, err := p.Probe(ctx, path)
	if err != nil {
		return err
	}

	if !result.HasAudio {
		return domain.NewError(domain.CodeVerifyFailed, domain.ClassLocal,
			"berkas hasil tidak memuat stream audio")
	}
	if kind == domain.KindVideo && !result.HasVideo {
		return domain.NewError(domain.CodeVerifyFailed, domain.ClassLocal,
			"berkas hasil tidak memuat stream video")
	}

	// Durasi sumber yang tidak diketahui tidak bisa dijadikan pembanding;
	// keberadaan stream audio sudah menjadi jaminan minimum.
	if expected <= 0 {
		return nil
	}

	diff := result.Duration - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > DurationTolerance {
		return domain.NewError(domain.CodeVerifyFailed, domain.ClassLocal,
			fmt.Sprintf("durasi hasil %s menyimpang dari sumber %s",
				result.Duration.Round(time.Second), expected.Round(time.Second)))
	}
	return nil
}

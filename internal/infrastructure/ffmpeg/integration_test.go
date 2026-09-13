//go:build integration

// Test integrasi dengan FFmpeg dan ffprobe sungguhan. Fixture dibuat
// sintetis oleh FFmpeg sendiri (nada sinus dan gambar polos), sehingga
// tidak ada media berhak cipta maupun URL publik yang dijadikan oracle.
//
//	go test -tags integration -run Integrasi ./internal/infrastructure/ffmpeg/
//
// Tool dicari seperti aplikasi mencarinya: direktori terkelola
// (YT2MP3_TEST_TOOLS_DIR bila diisi), sidecar, lalu PATH. Tanpa tool test
// dilewati, kecuali YT2MP3_REQUIRE_TOOLS=1 yang membuatnya gagal; CI memakai
// yang kedua supaya tidak pernah hijau tanpa menguji apa pun.

package ffmpeg_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/ffmpeg"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
)

const fixtureDuration = 5 * time.Second

type env struct {
	tools   *tools.Manager
	ffmpeg  string
	ffprobe string
	dir     string
	log     *slog.Logger
}

func setup(t *testing.T) env {
	t.Helper()

	toolsDir := os.Getenv("YT2MP3_TEST_TOOLS_DIR")
	if toolsDir == "" {
		toolsDir = filepath.Join(t.TempDir(), "tools")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	m, err := tools.New(toolsDir, t.TempDir(), log)
	if err != nil {
		t.Fatalf("tools.New() error = %v", err)
	}

	ctx := context.Background()
	ffmpegBin, version, err := m.Resolve(ctx, tools.FFmpeg)
	if err == nil {
		var probeErr error
		_, _, probeErr = m.Resolve(ctx, tools.FFprobe)
		err = probeErr
	}
	if err != nil {
		if os.Getenv("YT2MP3_REQUIRE_TOOLS") == "1" {
			t.Fatalf("ffmpeg/ffprobe wajib ada: %v", err)
		}
		t.Skipf("ffmpeg/ffprobe tidak ditemukan: %v", err)
	}
	ffprobeBin, _, _ := m.Resolve(ctx, tools.FFprobe)
	t.Logf("ffmpeg %s di %s", version, ffmpegBin)

	return env{tools: m, ffmpeg: ffmpegBin, ffprobe: ffprobeBin, dir: t.TempDir(), log: log}
}

// fixture membuat audio sumber dan sampul sintetis.
func (e env) fixture(t *testing.T) (audio, cover string) {
	t.Helper()
	audio = filepath.Join(e.dir, "sumber.wav")
	cover = filepath.Join(e.dir, "sampul.jpg")

	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command(e.ffmpeg, append([]string{"-hide_banner", "-y"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("buat fixture: %v\n%s", err, out)
		}
	}
	// Sumber sengaja 44,1 kHz supaya resampling ke 48 kHz ikut teruji.
	run("-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100:duration=5",
		"-ac", "2", "-c:a", "pcm_s16le", audio)
	// Sampul 16:9 yang lebih besar dari batas, seperti thumbnail YouTube,
	// supaya pemotongan persegi dan pengecilan ikut teruji.
	run("-f", "lavfi", "-i", "color=c=0x2e7d55:s=1600x900", "-frames:v", "1", cover)
	return audio, cover
}

func intPtr(v int) *int { return &v }

func media() *domain.MediaInfo {
	return &domain.MediaInfo{
		SourceKey: "youtube:dQw4w9WgXcQ",
		SourceURL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		Title:     "Nada Uji 水平線",
		Uploader:  "yt-to-mp3",
		Duration:  fixtureDuration,
	}
}

type streamInfo struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		SampleRate  string `json:"sample_rate"`
		Channels    int    `json:"channels"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
}

// inspect membaca detail yang tidak diekspos Prober: tag dan sampul.
func (e env) inspect(t *testing.T, path string) streamInfo {
	t.Helper()
	out, err := exec.Command(e.ffprobe, "-v", "error", "-print_format", "json",
		"-show_streams", "-show_format", path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var info streamInfo
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatalf("parse ffprobe: %v", err)
	}
	return info
}

func (e env) transcode(t *testing.T, preset *domain.Preset, audio, cover string) (string, int) {
	t.Helper()
	out := filepath.Join(e.dir, preset.ID+".mp3")
	var updates int

	err := ffmpeg.NewTranscoder(e.tools, e.log).Transcode(context.Background(), ffmpeg.TranscodeInput{
		AudioPath:  audio,
		CoverPath:  cover,
		OutputPath: out,
		Preset:     preset,
		Media:      media(),
		Timeout:    2 * time.Minute,
	}, func(ffmpeg.Progress) { updates++ })
	if err != nil {
		t.Fatalf("Transcode() error = %v", err)
	}
	return out, updates
}

func TestIntegrasiKonversiCBRLengkap(t *testing.T) {
	e := setup(t)
	audio, cover := e.fixture(t)

	out, updates := e.transcode(t, &domain.Preset{
		ID: "mp3_standard", Format: "mp3", Codec: "libmp3lame", Mode: "cbr",
		BitrateKbps: intPtr(192), SampleRate: intPtr(48000), Channels: 2,
	}, audio, cover)

	if updates == 0 {
		t.Error("tidak ada satu pun laporan progress dari ffmpeg")
	}

	prober := ffmpeg.NewProber(e.tools, e.log)
	res, err := prober.Probe(context.Background(), out)
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if !res.HasAudio || res.Codec != "mp3" || res.SampleRate != 48000 {
		t.Errorf("hasil = %+v, mau mp3 48000 Hz", res)
	}
	if err := prober.Verify(context.Background(), out, fixtureDuration); err != nil {
		t.Errorf("Verify() error = %v", err)
	}

	info := e.inspect(t, out)
	var audioStreams, pictures int
	for _, s := range info.Streams {
		switch {
		case s.CodecType == "audio":
			audioStreams++
			if s.Channels != 2 {
				t.Errorf("channels = %d, mau 2", s.Channels)
			}
		case s.Disposition.AttachedPic == 1:
			pictures++
			if s.Width != 800 || s.Height != 800 {
				t.Errorf("sampul = %dx%d, mau 800x800", s.Width, s.Height)
			}
		}
	}
	if audioStreams != 1 || pictures != 1 {
		t.Errorf("stream audio = %d, sampul = %d; mau 1 dan 1", audioStreams, pictures)
	}
	// Judul non-ASCII menguji bahwa tag tidak rusak saat melewati argumen
	// proses di Windows.
	if got := info.Format.Tags["title"]; got != "Nada Uji 水平線" {
		t.Errorf("tag title = %q", got)
	}
	if got := info.Format.Tags["artist"]; got != "yt-to-mp3" {
		t.Errorf("tag artist = %q", got)
	}
}

func TestIntegrasiKonversiVBR(t *testing.T) {
	e := setup(t)
	audio, cover := e.fixture(t)

	out, _ := e.transcode(t, &domain.Preset{
		ID: "mp3_vbr_v0", Format: "mp3", Codec: "libmp3lame", Mode: "vbr",
		VBRQuality: intPtr(0), SampleRate: intPtr(48000), Channels: 2,
	}, audio, cover)

	if err := ffmpeg.NewProber(e.tools, e.log).Verify(context.Background(), out, fixtureDuration); err != nil {
		t.Errorf("Verify() error = %v", err)
	}
}

// Hasil yang jauh lebih pendek dari sumber, misalnya unduhan terpotong,
// tidak boleh lolos sebagai sukses.
func TestIntegrasiVerifyMenolakDurasiMenyimpang(t *testing.T) {
	e := setup(t)
	audio, cover := e.fixture(t)

	out, _ := e.transcode(t, &domain.Preset{
		ID: "mp3_economy", Format: "mp3", Codec: "libmp3lame", Mode: "cbr",
		BitrateKbps: intPtr(128), SampleRate: intPtr(48000), Channels: 2,
	}, audio, cover)

	err := ffmpeg.NewProber(e.tools, e.log).Verify(context.Background(), out, time.Minute)
	var derr *domain.Error
	if !errors.As(err, &derr) || derr.Code != domain.CodeVerifyFailed {
		t.Errorf("Verify() error = %v, mau %s", err, domain.CodeVerifyFailed)
	}
}

// Thumbnail rusak tidak boleh menggagalkan job: konversi diulang tanpa
// sampul dan hasilnya tetap berkas audio yang sah.
func TestIntegrasiSampulRusakTidakMenggagalkan(t *testing.T) {
	e := setup(t)
	audio, _ := e.fixture(t)
	broken := filepath.Join(e.dir, "rusak.jpg")
	if err := os.WriteFile(broken, []byte("bukan gambar"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := e.transcode(t, &domain.Preset{
		ID: "mp3_standard", Format: "mp3", Codec: "libmp3lame", Mode: "cbr",
		BitrateKbps: intPtr(192), SampleRate: intPtr(48000), Channels: 2,
	}, audio, broken)

	info := e.inspect(t, out)
	for _, s := range info.Streams {
		if s.Disposition.AttachedPic == 1 {
			t.Error("sampul rusak seharusnya tidak ikut disematkan")
		}
	}
	if err := ffmpeg.NewProber(e.tools, e.log).Verify(context.Background(), out, fixtureDuration); err != nil {
		t.Errorf("Verify() error = %v", err)
	}
}

// Tag tahun dan album dari data rilis harus benar-benar terbaca dari berkas
// ID3v2.3, bukan hanya ada di argv.
func TestIntegrasiTagDataRilis(t *testing.T) {
	e := setup(t)
	audio, cover := e.fixture(t)
	out := filepath.Join(e.dir, "rilis.mp3")

	info := media()
	info.Artist, info.Album, info.ReleaseYear = "Yiruma", "The Best", 2011
	err := ffmpeg.NewTranscoder(e.tools, e.log).Transcode(context.Background(), ffmpeg.TranscodeInput{
		AudioPath: audio, CoverPath: cover, OutputPath: out, Timeout: 2 * time.Minute, Media: info,
		Preset: &domain.Preset{ID: "mp3_standard", Format: "mp3", Codec: "libmp3lame", Mode: "cbr",
			BitrateKbps: intPtr(192), SampleRate: intPtr(48000), Channels: 2},
	}, func(ffmpeg.Progress) {})
	if err != nil {
		t.Fatalf("Transcode() error = %v", err)
	}

	tags := e.inspect(t, out).Format.Tags
	if tags["artist"] != "Yiruma" || tags["album"] != "The Best" || tags["date"] != "2011" {
		t.Errorf("tag = %v", tags)
	}
}

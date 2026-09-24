package application

import (
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

func intPtr(v int) *int { return &v }

// Resolusi yang diperkirakan adalah batas preset, kecuali sumbernya sendiri
// lebih rendah; tanpa keduanya dianggap 1080p.
func TestVideoHeightFor(t *testing.T) {
	tests := []struct {
		name   string
		max    *int
		source int
		want   int
	}{
		{"batas preset", intPtr(720), 2160, 720},
		{"sumber lebih rendah", intPtr(1080), 480, 480},
		{"sumber tidak diketahui", intPtr(480), 0, 480},
		{"terbaik mengikuti sumber", nil, 2160, 2160},
		{"terbaik tanpa info sumber", nil, 0, assumedVideoHeight},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preset := &domain.Preset{Kind: domain.KindVideo, MaxHeight: tc.max}
			got := videoHeightFor(preset, &domain.MediaInfo{VideoHeight: tc.source})
			if got != tc.want {
				t.Errorf("videoHeightFor() = %d, mau %d", got, tc.want)
			}
		})
	}
}

// Perkiraan bitrate wajib naik bersama resolusi; kalau tidak, preflight
// meloloskan video 4K yang pasti tidak muat.
func TestVideoKbpsNaikBersamaResolusi(t *testing.T) {
	last := 0
	for _, h := range []int{360, 480, 720, 1080, 1440, 2160} {
		kbps := videoKbps(h)
		if kbps <= last {
			t.Errorf("videoKbps(%d) = %d, tidak lebih besar dari resolusi sebelumnya (%d)", h, kbps, last)
		}
		last = kbps
	}
}

// Video mendapat batas waktu jauh lebih longgar: unduhannya belasan kali
// lebih besar dan encode ulang 4K bisa lebih lambat dari waktu nyata.
func TestLimitsForVideoLebihLonggar(t *testing.T) {
	const duration = 10 * time.Minute
	audio := limitsFor(&domain.Preset{Kind: domain.KindAudio}, duration)
	video := limitsFor(&domain.Preset{Kind: domain.KindVideo}, duration)

	if audio.download != 30*time.Minute || audio.convert != 10*time.Minute {
		t.Errorf("batas audio = %+v, mau 30m unduh dan 10m konversi", audio)
	}
	if video.download != 100*time.Minute || video.convert != 60*time.Minute {
		t.Errorf("batas video = %+v, mau 100m unduh dan 60m konversi", video)
	}

	// Video pendek tetap mendapat batas minimum.
	short := limitsFor(&domain.Preset{Kind: domain.KindVideo}, 30*time.Second)
	if short.download != minVideoDownloadTime || short.convert != minVideoConvertTime {
		t.Errorf("batas video pendek = %+v", short)
	}
}

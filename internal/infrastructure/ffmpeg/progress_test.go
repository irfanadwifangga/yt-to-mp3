package ffmpeg

import (
	"strings"
	"testing"
	"time"
)

// Blok progress nyata dari FFmpeg 9.0.1. Perhatikan out_time_ms dan
// out_time_us melaporkan angka yang sama: keduanya mikrodetik.
const realOutput = `bitrate=  32.0kbits/s
total_size=8192
out_time_us=2000000
out_time_ms=2000000
out_time=00:00:02.000000
speed= 145x
progress=continue
bitrate=  32.0kbits/s
total_size=20480
out_time_us=5000000
out_time_ms=5000000
out_time=00:00:05.000000
progress=end
`

func TestParseProgress(t *testing.T) {
	var got []Progress
	if err := ParseProgress(strings.NewReader(realOutput), func(p Progress) {
		got = append(got, p)
	}); err != nil {
		t.Fatalf("ParseProgress() error = %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("jumlah pembaruan = %d, mau 2: %+v", len(got), got)
	}

	// Inilah jebakannya: 2000000 pada out_time_ms berarti 2 detik, bukan
	// 2000 detik. Salah menafsirkannya membuat progress meleset 1000 kali.
	if got[0].OutTime != 2*time.Second {
		t.Errorf("OutTime pertama = %v, mau 2s", got[0].OutTime)
	}
	if got[0].Done {
		t.Error("pembaruan pertama seharusnya belum selesai")
	}

	if got[1].OutTime != 5*time.Second {
		t.Errorf("OutTime kedua = %v, mau 5s", got[1].OutTime)
	}
	if !got[1].Done {
		t.Error("pembaruan kedua seharusnya menandai selesai")
	}
}

// Satu pembaruan per blok, bukan per baris, supaya konsumen tidak menerima
// keadaan setengah terbaca.
func TestParseProgressSatuPembaruanPerBlok(t *testing.T) {
	input := "out_time_us=1000000\nbitrate=32\nspeed=10x\nprogress=continue\n"

	var count int
	if err := ParseProgress(strings.NewReader(input), func(Progress) { count++ }); err != nil {
		t.Fatalf("ParseProgress() error = %v", err)
	}
	if count != 1 {
		t.Errorf("jumlah pembaruan = %d, mau 1", count)
	}
}

func TestParseProgressMengabaikanBarisRusak(t *testing.T) {
	input := "baris tanpa sama dengan\nout_time_us=bukan angka\nout_time_us=3000000\nprogress=end\n"

	var got []Progress
	if err := ParseProgress(strings.NewReader(input), func(p Progress) { got = append(got, p) }); err != nil {
		t.Fatalf("ParseProgress() error = %v", err)
	}
	if len(got) != 1 || got[0].OutTime != 3*time.Second {
		t.Errorf("hasil = %+v, mau satu pembaruan 3s", got)
	}
}

func TestPercent(t *testing.T) {
	tests := []struct {
		name  string
		out   time.Duration
		total time.Duration
		want  *float64
	}{
		{"setengah", 30 * time.Second, time.Minute, ptr(50)},
		{"awal", 0, time.Minute, ptr(0)},
		{"penuh", time.Minute, time.Minute, ptr(100)},
		{"melewati durasi dijepit", 2 * time.Minute, time.Minute, ptr(100)},
		{"durasi tidak diketahui", 30 * time.Second, 0, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Progress{OutTime: tc.out}.Percent(tc.total)
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("Percent() = %v, mau nil (indeterminate)", *got)
			case tc.want != nil && got == nil:
				t.Errorf("Percent() = nil, mau %v", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Errorf("Percent() = %v, mau %v", *got, *tc.want)
			}
		})
	}
}

func ptr(v float64) *float64 { return &v }

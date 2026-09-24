package ytdlp

import (
	"strings"
	"testing"
)

func TestParseProgress(t *testing.T) {
	input := strings.Join([]string{
		"[youtube] Extracting URL: https://example.invalid",
		"YTDLP_PROGRESS 1048576 10485760 NA",
		"YTDLP_PROGRESS 5242880 10485760 NA",
		"[download] 100% of 10.00MiB",
		"YTDLP_PROGRESS 10485760 10485760 NA",
	}, "\n")

	var got []Progress
	var other []string
	if err := ParseProgress(strings.NewReader(input),
		func(p Progress) { got = append(got, p) },
		func(s string) { other = append(other, s) },
	); err != nil {
		t.Fatalf("ParseProgress() error = %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("jumlah progress = %d, mau 3: %+v", len(got), got)
	}
	if got[0].Downloaded != 1048576 || got[0].Total != 10485760 {
		t.Errorf("progress pertama = %+v", got[0])
	}
	if pct := got[1].Percent(); pct == nil || *pct != 50 {
		t.Errorf("persentase kedua = %v, mau 50", pct)
	}

	// Baris non-progress harus tetap bisa dipanen untuk log, bukan dibuang.
	if len(other) != 2 {
		t.Errorf("baris lain = %d, mau 2: %v", len(other), other)
	}
}

// Ukuran pasti lebih dipercaya daripada estimasi; estimasi hanya dipakai
// ketika yang pasti belum tersedia.
func TestParseProgressMemilihTotal(t *testing.T) {
	tests := map[string]struct {
		line string
		want int64
	}{
		"pakai total pasti":    {"YTDLP_PROGRESS 100 5000 9999", 5000},
		"jatuh ke estimasi":    {"YTDLP_PROGRESS 100 NA 7000", 7000},
		"keduanya NA":          {"YTDLP_PROGRESS 100 NA NA", 0},
		"estimasi pecahan":     {"YTDLP_PROGRESS 100 NA 7000.5", 7000},
		"tanpa kolom estimasi": {"YTDLP_PROGRESS 100 5000", 5000},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			p, ok := parseProgressLine(tc.line)
			if !ok {
				t.Fatalf("baris %q tidak terbaca", tc.line)
			}
			if p.Total != tc.want {
				t.Errorf("Total = %d, mau %d", p.Total, tc.want)
			}
		})
	}
}

// Total yang belum diketahui berarti indeterminate, bukan nol persen.
func TestPercentIndeterminate(t *testing.T) {
	if pct := (Progress{Downloaded: 500, Total: 0}).Percent(); pct != nil {
		t.Errorf("Percent() = %v, mau nil", *pct)
	}
	if pct := (Progress{Downloaded: 500, Total: 1000}).Percent(); pct == nil || *pct != 50 {
		t.Errorf("Percent() = %v, mau 50", pct)
	}
	// Server yang melaporkan ukuran lebih kecil dari yang terunduh tidak
	// boleh menghasilkan angka di atas 100.
	if pct := (Progress{Downloaded: 2000, Total: 1000}).Percent(); pct == nil || *pct != 100 {
		t.Errorf("Percent() = %v, mau dijepit ke 100", pct)
	}
}

func TestParseProgressBarisRusak(t *testing.T) {
	input := "YTDLP_PROGRESS\nYTDLP_PROGRESS bukanangka 100\nYTDLP_PROGRESS 50 100\n"

	var got []Progress
	if err := ParseProgress(strings.NewReader(input),
		func(p Progress) { got = append(got, p) }, nil); err != nil {
		t.Fatalf("ParseProgress() error = %v", err)
	}
	if len(got) != 1 || got[0].Downloaded != 50 {
		t.Errorf("hasil = %+v, mau satu progress 50", got)
	}
}

// Flag hardening pada jalur unduhan sama pentingnya dengan pada metadata:
// tanpa --ignore-config, yt-dlp.conf milik pengguna dapat menyuntikkan
// --exec dan menjadikan ini jalur eksekusi perintah sewenang-wenang.
func TestDownloadArgsHardening(t *testing.T) {
	args := downloadArgs("https://www.youtube.com/watch?v=dQw4w9WgXcQ", "/tmp/job", "/opt/ffmpeg/ffmpeg", Selection{})

	required := []string{"--ignore-config", "--no-exec", "--no-playlist", "-f", "bestaudio/best"}
	for _, want := range required {
		if !contains(args, want) {
			t.Errorf("argv tidak memuat %s", want)
		}
	}

	// URL wajib tepat setelah "--" supaya tidak pernah terbaca sebagai flag.
	sep := indexOf(args, "--")
	if sep == -1 || sep != len(args)-2 {
		t.Errorf("separator -- pada indeks %d, mau %d", sep, len(args)-2)
	}
	if !strings.HasPrefix(args[len(args)-1], "https://") {
		t.Errorf("argumen terakhir bukan URL: %q", args[len(args)-1])
	}
}

func contains(list []string, want string) bool { return indexOf(list, want) != -1 }

func indexOf(list []string, want string) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

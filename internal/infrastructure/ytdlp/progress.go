package ytdlp

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// progressPrefix menandai baris progress buatan kita sendiri, sehingga
// keluaran lain dari yt-dlp tidak pernah salah terbaca sebagai progress.
const progressPrefix = "YTDLP_PROGRESS"

// progressTemplate meminta yt-dlp mencetak angka mentah.
//
// total_bytes dan total_bytes_estimate diminta terpisah: unduhan
// terfragmentasi sering hanya punya estimasi, sementara berkas biasa punya
// ukuran pasti. Keduanya bernilai "NA" bila tidak diketahui.
const progressTemplate = "download:" + progressPrefix +
	" %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s"

// Progress adalah satu pembaruan unduhan.
type Progress struct {
	Downloaded int64

	// Total bernilai nol ketika ukuran akhir belum diketahui, misalnya pada
	// unduhan terfragmentasi di awal.
	Total int64
}

// Percent menghitung kemajuan unduhan.
//
// Total yang belum diketahui menghasilkan nil, yang berarti indeterminate.
func (p Progress) Percent() *float64 {
	if p.Total <= 0 {
		return nil
	}
	pct := float64(p.Downloaded) / float64(p.Total) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return &pct
}

// ParseProgress membaca stdout yt-dlp dan memanggil emit untuk setiap baris
// progress. Baris lain diteruskan ke onOther bila disediakan.
func ParseProgress(r io.Reader, emit func(Progress), onOther func(string)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if !strings.HasPrefix(line, progressPrefix) {
			if onOther != nil && line != "" {
				onOther(line)
			}
			continue
		}

		if p, ok := parseProgressLine(line); ok {
			emit(p)
		}
	}
	return scanner.Err()
}

// parseProgressLine membaca satu baris "YTDLP_PROGRESS <unduh> <total> <estimasi>".
func parseProgressLine(line string) (Progress, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return Progress{}, false
	}

	downloaded, ok := parseNumber(fields[1])
	if !ok {
		return Progress{}, false
	}

	// Ukuran pasti lebih dipercaya daripada estimasi; estimasi hanya dipakai
	// ketika yang pasti belum tersedia.
	var total int64
	if len(fields) > 2 {
		if v, ok := parseNumber(fields[2]); ok {
			total = v
		}
	}
	if total == 0 && len(fields) > 3 {
		if v, ok := parseNumber(fields[3]); ok {
			total = v
		}
	}

	return Progress{Downloaded: downloaded, Total: total}, true
}

// parseNumber menerima angka bulat; "NA" dan nilai lain dianggap tidak ada.
func parseNumber(s string) (int64, bool) {
	if s == "" || s == "NA" || s == "None" {
		return 0, false
	}
	// yt-dlp kadang mencetak pecahan untuk estimasi.
	if f, err := strconv.ParseFloat(s, 64); err == nil && f >= 0 {
		return int64(f), true
	}
	return 0, false
}

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
//
// vcodec dan acodec menandai isi berkas yang sedang diunduh. Unduhan video
// terdiri dari dua berkas terpisah, video lalu audio, yang masing-masing
// melaporkan progres 0 sampai 100; tanpa penanda ini bar progres melompat
// kembali ke nol di tengah jalan.
const progressTemplate = "download:" + progressPrefix +
	" %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s" +
	" %(info.vcodec)s %(info.acodec)s"

// Stream adalah isi berkas yang sedang diunduh.
type Stream int

const (
	StreamUnknown  Stream = iota // yt-dlp tidak menyebutkan codec
	StreamCombined               // video dan audio dalam satu berkas
	StreamVideo                  // video saja, audionya diunduh terpisah
	StreamAudio                  // audio saja
)

// Progress adalah satu pembaruan unduhan.
type Progress struct {
	Downloaded int64

	// Total bernilai nol ketika ukuran akhir belum diketahui, misalnya pada
	// unduhan terfragmentasi di awal.
	Total int64

	Stream Stream

	// placed, from, dan span memetakan progres satu berkas ke rentangnya
	// pada unduhan keseluruhan. Diisi partTracker; progres yang tidak
	// ditempatkan mencakup seluruh rentang 0 sampai 100.
	placed     bool
	from, span float64
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
	if p.placed {
		pct = p.from + pct/100*p.span
	}
	return &pct
}

// videoShare adalah porsi stream video pada unduhan video yang terpisah
// dari audionya. Ukuran sebenarnya baru diketahui saat tiap berkas mulai
// diunduh, sedangkan pada 360p pun video sudah beberapa kali lipat ukuran
// audio 128 kbps, jadi porsi tetap ini cukup dekat tanpa membuat progres
// mundur.
const videoShare = 90.0

// partTracker menggabungkan progres beberapa berkas unduhan menjadi satu
// progres yang tidak pernah mundur, apa pun urutan video dan audionya.
type partTracker struct {
	started bool
	current Stream
	done    float64
	span    float64
}

// place menempatkan progres satu berkas pada rentang keseluruhan.
func (t *partTracker) place(p Progress) Progress {
	if !t.started || p.Stream != t.current {
		if t.started {
			t.done += t.span
		}
		t.started = true
		t.current = p.Stream
		t.span = min(shareOf(p.Stream), 100-t.done)
	}
	p.placed, p.from, p.span = true, t.done, t.span
	return p
}

func shareOf(s Stream) float64 {
	switch s {
	case StreamVideo:
		return videoShare
	case StreamAudio:
		return 100 - videoShare
	default:
		return 100
	}
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

// parseProgressLine membaca satu baris
// "YTDLP_PROGRESS <unduh> <total> <estimasi> <vcodec> <acodec>".
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

	p := Progress{Downloaded: downloaded, Total: total}
	if len(fields) > 5 {
		p.Stream = streamOf(fields[4], fields[5])
	}
	return p, true
}

// streamOf menebak isi berkas dari codec yang dilaporkan yt-dlp. "none"
// berarti stream itu memang tidak ada; "NA" berarti tidak diketahui.
func streamOf(vcodec, acodec string) Stream {
	known := func(c string) bool { return c != "" && c != "NA" && c != "None" }
	switch {
	case vcodec == "none" && known(acodec):
		return StreamAudio
	case acodec == "none" && known(vcodec):
		return StreamVideo
	case known(vcodec) && known(acodec):
		return StreamCombined
	default:
		return StreamUnknown
	}
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

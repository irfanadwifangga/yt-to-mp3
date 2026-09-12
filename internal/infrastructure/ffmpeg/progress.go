// Package ffmpeg membungkus FFmpeg: pembangunan argv, parsing progress, dan
// tagging.
package ffmpeg

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// Progress adalah satu pembaruan dari keluaran -progress.
type Progress struct {
	// OutTime adalah posisi encoding saat ini.
	OutTime time.Duration

	// Done menandai baris progress=end.
	Done bool
}

// Percent menghitung kemajuan terhadap durasi total.
//
// Durasi nol atau tidak diketahui menghasilkan nil, yang berarti
// indeterminate. Itu nilai yang sah, bukan data hilang.
func (p Progress) Percent(total time.Duration) *float64 {
	if total <= 0 {
		return nil
	}
	pct := float64(p.OutTime) / float64(total) * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return &pct
}

// ParseProgress membaca aliran -progress dan memanggil emit setiap kali ada
// pembaruan.
//
// FFmpeg menulis blok key=value yang diakhiri baris progress=. Satu
// pembaruan dikirim per blok, bukan per baris, supaya konsumen tidak
// menerima keadaan setengah terbaca.
func ParseProgress(r io.Reader, emit func(Progress)) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var current Progress
	var haveTime bool

	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "out_time_us", "out_time_ms":
			// Keduanya dilaporkan dalam MIKRODETIK. Nama out_time_ms adalah
			// kekeliruan lama di FFmpeg yang tidak pernah diperbaiki demi
			// kompatibilitas; memperlakukannya sebagai milidetik membuat
			// progress meleset 1000 kali.
			if micros, err := strconv.ParseInt(value, 10, 64); err == nil && micros >= 0 {
				current.OutTime = time.Duration(micros) * time.Microsecond
				haveTime = true
			}

		case "progress":
			current.Done = value == "end"
			if haveTime || current.Done {
				emit(current)
			}
			current = Progress{}
			haveTime = false
		}
	}
	return scanner.Err()
}

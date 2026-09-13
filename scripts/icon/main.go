// Command icon membuat ikon aplikasi Windows dari tanda merek SPA.
//
//	go run ./scripts/icon -o packaging/windows/yt-to-mp3.ico
//
// Bentuknya sama dengan .brand-mark di web/src/index.css: kotak membulat
// yang terisi hijau dari bawah seperti level meter. Ikon digambar dari kode,
// bukan disimpan sebagai berkas desain, supaya proporsinya tidak menyimpang
// dari UI. Hasilnya di-commit; jalankan ulang hanya bila tanda merek berubah.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

var (
	// signal sama dengan --signal pada tema terang.
	signal = color.NRGBA{R: 0x2e, G: 0x7d, B: 0x55, A: 0xff}
	// track sengaja lebih gelap daripada --line di UI: ikon harus tetap
	// terbaca di taskbar terang maupun gelap, sedangkan --line nyaris
	// hilang di latar putih.
	track = color.NRGBA{R: 0x6f, G: 0x7a, B: 0x73, A: 0xff}
)

// fillRatio sama dengan batas 55% pada gradient .brand-mark.
const fillRatio = 0.55

// sizes mencakup ukuran yang diminta Explorer, taskbar, dan Start Menu pada
// berbagai skala DPI.
var sizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

func main() {
	out := flag.String("o", "packaging/windows/yt-to-mp3.ico", "berkas ICO keluaran")
	flag.Parse()

	data, err := buildICO(sizes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "susun ikon:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "tulis ikon:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: %d ukuran, %d byte\n", *out, len(sizes), len(data))
}

// render menggambar tanda merek pada kanvas persegi berukuran size.
func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	s := float64(size)

	// Sedikit ruang kosong di tepi, seperti ikon Windows lain, supaya ikon
	// tidak tampak lebih besar daripada tetangganya di taskbar.
	pad := math.Max(1, math.Round(s/16))
	x0, y0, x1, y1 := pad, pad, s-pad, s-pad
	radius := (x1 - x0) * 0.17 // 3px pada kotak 1,1rem di UI
	split := y1 - (y1-y0)*fillRatio

	// Supersampling per piksel untuk tepi sudut yang halus.
	const ss = 4
	for py := range size {
		for px := range size {
			var green, gray int
			for sy := range ss {
				for sx := range ss {
					x := float64(px) + (float64(sx)+0.5)/ss
					y := float64(py) + (float64(sy)+0.5)/ss
					if !insideRounded(x, y, x0, y0, x1, y1, radius) {
						continue
					}
					if y >= split {
						green++
					} else {
						gray++
					}
				}
			}
			covered := green + gray
			if covered == 0 {
				continue
			}
			c := mix(signal, track, green, gray)
			c.A = uint8(255 * covered / (ss * ss))
			img.SetNRGBA(px, py, c)
		}
	}
	return img
}

// mix mencampur dua warna sesuai porsi sampel masing-masing.
func mix(a, b color.NRGBA, na, nb int) color.NRGBA {
	n := na + nb
	ch := func(x, y uint8) uint8 { return uint8((int(x)*na + int(y)*nb) / n) }
	return color.NRGBA{R: ch(a.R, b.R), G: ch(a.G, b.G), B: ch(a.B, b.B), A: 0xff}
}

// insideRounded melaporkan apakah titik berada di dalam persegi bersudut
// membulat.
func insideRounded(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Min(math.Max(x, x0+r), x1-r)
	cy := math.Min(math.Max(y, y0+r), y1-r)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// icoEntry adalah satu entri direktori ICO.
type icoEntry struct {
	Width, Height   uint8 // 0 berarti 256
	Colors, Reserve uint8
	Planes          uint16
	BitCount        uint16
	Size, Offset    uint32
}

// buildICO menyusun berkas ICO berisi PNG untuk setiap ukuran. Entri PNG
// didukung sejak Windows Vista dan jauh lebih kecil daripada bitmap mentah.
func buildICO(sizes []int) ([]byte, error) {
	images := make([][]byte, len(sizes))
	for i, s := range sizes {
		if s < 1 || s > 256 {
			return nil, fmt.Errorf("ukuran %d di luar 1..256", s)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, render(s)); err != nil {
			return nil, err
		}
		images[i] = buf.Bytes()
	}

	var out bytes.Buffer
	le := binary.LittleEndian
	// ICONDIR: reserved, type 1 (ikon), jumlah gambar.
	_ = binary.Write(&out, le, [3]uint16{0, 1, uint16(len(sizes))})

	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := uint8(s % 256)
		_ = binary.Write(&out, le, icoEntry{
			Width: dim, Height: dim, Planes: 1, BitCount: 32,
			Size: uint32(len(images[i])), Offset: uint32(offset),
		})
		offset += len(images[i])
	}
	for _, img := range images {
		out.Write(img)
	}
	return out.Bytes(), nil
}

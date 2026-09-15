// Command icon menggambar ikon aplikasi dari satu definisi geometri.
//
//	go run ./scripts/icon -o packaging/windows/yt-to-mp3.ico -png-dir web/public -svg web/public/favicon.svg
//
// Bentuknya menggabungkan ciri khas UI dengan tanda audio: sampul yang
// terisi warna dari bawah (seperti sampul job yang sedang dikonversi) dan
// gelombang suara tiga bar di atasnya, "video menjadi audio". ICO, PNG, dan
// SVG diturunkan dari definisi yang sama supaya tidak pernah menyimpang satu
// sama lain. Hasilnya di-commit; jalankan ulang hanya bila tanda merek
// berubah.
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
	"path/filepath"
	"strings"
)

var (
	// cover adalah sampul yang belum terisi. Di taskbar gelap bagian navy
	// ini menyatu dengan latar; bentuk ikon tetap terbaca lewat isian biru
	// dan bar terang. Dipilih pengguna dari perbandingan beberapa palet.
	cover = color.NRGBA{R: 0x13, G: 0x23, B: 0x3f, A: 0xff}
	// fill satu keluarga dengan --signal UI (#2563eb terang, #60a5fa gelap).
	fill = color.NRGBA{R: 0x3b, G: 0x82, B: 0xf6, A: 0xff}
	// wave terang supaya kontras di atas sampul navy maupun isian biru.
	wave = color.NRGBA{R: 0xe8, G: 0xf1, B: 0xff, A: 0xff}
)

// Geometri dalam satuan sisi tile (0..1).
const (
	cornerRadius = 0.2
	fillLevel    = 0.5 // tinggi bagian terisi dari bawah
	barWidth     = 0.16
	barGap       = 0.11
)

// barHeights membentuk gelombang: bar tengah paling tinggi. Tiga bar, bukan
// lebih, supaya bentuknya masih terbaca di ukuran 16 px.
var barHeights = []float64{0.34, 0.6, 0.42}

// sizes mencakup ukuran yang diminta Explorer, taskbar, dan Start Menu pada
// berbagai skala DPI.
var sizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

// pngSizes adalah ikon PNG untuk SPA. Jendela aplikasi Edge memakai ikon
// halaman untuk judul jendela dan taskbar.
var pngSizes = []int{32, 192}

func main() {
	out := flag.String("o", "packaging/windows/yt-to-mp3.ico", "berkas ICO keluaran")
	pngDir := flag.String("png-dir", "", "direktori untuk icon-32.png dan icon-192.png (opsional)")
	svgOut := flag.String("svg", "", "berkas favicon SVG (opsional)")
	preview := flag.String("preview", "", "PNG pratinjau di latar terang dan gelap (opsional)")
	flag.Parse()

	data, err := buildICO(sizes)
	if err != nil {
		fail("susun ikon", err)
	}
	write(*out, data)

	if *pngDir != "" {
		for _, s := range pngSizes {
			write(filepath.Join(*pngDir, fmt.Sprintf("icon-%d.png", s)), encodePNG(render(s)))
		}
	}
	if *svgOut != "" {
		write(*svgOut, []byte(buildSVG()))
	}
	if *preview != "" {
		write(*preview, encodePNG(previewSheet()))
	}
}

func write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fail("tulis "+path, err)
	}
	fmt.Printf("%s: %d byte\n", path, len(data))
}

func fail(what string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
	os.Exit(1)
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fail("susun PNG", err)
	}
	return buf.Bytes()
}

// layout adalah geometri ikon pada kanvas berukuran tertentu.
type layout struct {
	x0, y0, side, radius float64
	split                float64 // batas atas bagian hijau
	bars                 []bar
}

type bar struct{ cx, cy, w, h float64 }

func layoutFor(size float64) layout {
	// Sedikit ruang kosong di tepi, seperti ikon Windows lain, supaya ikon
	// tidak tampak lebih besar daripada tetangganya di taskbar.
	pad := math.Max(1, math.Round(size/16))
	side := size - 2*pad
	l := layout{
		x0: pad, y0: pad, side: side,
		radius: side * cornerRadius,
		split:  pad + side*(1-fillLevel),
	}

	total := float64(len(barHeights))*barWidth + float64(len(barHeights)-1)*barGap
	x := pad + side*(1-total)/2
	for _, h := range barHeights {
		w := side * barWidth
		l.bars = append(l.bars, bar{cx: x + w/2, cy: pad + side/2, w: w, h: side * h})
		x += w + side*barGap
	}
	return l
}

// colorAt mengembalikan warna titik dan apakah titik itu bagian dari ikon.
func (l layout) colorAt(x, y float64) (color.NRGBA, bool) {
	if !insideRounded(x, y, l.x0, l.y0, l.x0+l.side, l.y0+l.side, l.radius) {
		return color.NRGBA{}, false
	}
	for _, b := range l.bars {
		if insideCapsule(x, y, b) {
			return wave, true
		}
	}
	if y >= l.split {
		return fill, true
	}
	return cover, true
}

// render menggambar ikon pada kanvas persegi berukuran size.
func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	l := layoutFor(float64(size))

	// Supersampling per piksel untuk tepi yang halus: warna dirata-rata dari
	// sampel yang mengenai ikon, alpha dari porsi sampel itu.
	const ss = 4
	for py := range size {
		for px := range size {
			var r, g, b, hit int
			for sy := range ss {
				for sx := range ss {
					c, ok := l.colorAt(float64(px)+(float64(sx)+0.5)/ss, float64(py)+(float64(sy)+0.5)/ss)
					if !ok {
						continue
					}
					r, g, b, hit = r+int(c.R), g+int(c.G), b+int(c.B), hit+1
				}
			}
			if hit == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(r / hit), G: uint8(g / hit), B: uint8(b / hit),
				A: uint8(255 * hit / (ss * ss)),
			})
		}
	}
	return img
}

// buildSVG menulis geometri yang sama sebagai favicon vektor.
func buildSVG() string {
	const size = 64
	l := layoutFor(size)
	hex := func(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }
	f := func(v float64) string {
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
	}

	var s strings.Builder
	fmt.Fprintf(&s, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d">`+"\n", size, size)
	s.WriteString("  <!-- Dibuat scripts/icon; ubah geometrinya di sana, bukan di berkas ini. -->\n")
	fmt.Fprintf(&s, `  <clipPath id="tile"><rect x="%s" y="%s" width="%s" height="%s" rx="%s"/></clipPath>`+"\n",
		f(l.x0), f(l.y0), f(l.side), f(l.side), f(l.radius))
	s.WriteString(`  <g clip-path="url(#tile)">` + "\n")
	fmt.Fprintf(&s, `    <rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n",
		f(l.x0), f(l.y0), f(l.side), f(l.split-l.y0), hex(cover))
	fmt.Fprintf(&s, `    <rect x="%s" y="%s" width="%s" height="%s" fill="%s"/>`+"\n",
		f(l.x0), f(l.split), f(l.side), f(l.y0+l.side-l.split), hex(fill))
	s.WriteString("  </g>\n")
	for _, b := range l.bars {
		fmt.Fprintf(&s, `  <rect x="%s" y="%s" width="%s" height="%s" rx="%s" fill="%s"/>`+"\n",
			f(b.cx-b.w/2), f(b.cy-b.h/2), f(b.w), f(b.h), f(b.w/2), hex(wave))
	}
	s.WriteString("</svg>\n")
	return s.String()
}

// previewSheet menampilkan ikon pada ukuran kecil yang diperbesar tanpa
// penghalusan, di atas latar taskbar terang dan gelap.
func previewSheet() *image.NRGBA {
	previewSizes := []int{16, 24, 32, 48}
	const scale, gap = 6, 24
	backgrounds := []color.NRGBA{{0xf3, 0xf3, 0xf3, 0xff}, {0x20, 0x20, 0x20, 0xff}}

	width := gap
	for _, s := range previewSizes {
		width += s*scale + gap
	}
	rowH := 48*scale + 2*gap
	sheet := image.NewNRGBA(image.Rect(0, 0, width, rowH*len(backgrounds)))

	for row, bg := range backgrounds {
		for y := row * rowH; y < (row+1)*rowH; y++ {
			for x := range width {
				sheet.SetNRGBA(x, y, bg)
			}
		}
		x := gap
		for _, s := range previewSizes {
			icon := render(s)
			top := row*rowH + gap + (48-s)*scale/2
			for iy := range s * scale {
				for ix := range s * scale {
					c := icon.NRGBAAt(ix/scale, iy/scale)
					sheet.SetNRGBA(x+ix, top+iy, blend(bg, c))
				}
			}
			x += s*scale + gap
		}
	}
	return sheet
}

func blend(bg, fg color.NRGBA) color.NRGBA {
	a := int(fg.A)
	ch := func(b, f uint8) uint8 { return uint8((int(f)*a + int(b)*(255-a)) / 255) }
	return color.NRGBA{R: ch(bg.R, fg.R), G: ch(bg.G, fg.G), B: ch(bg.B, fg.B), A: 0xff}
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

// insideCapsule melaporkan apakah titik berada di dalam bar berujung bulat.
func insideCapsule(x, y float64, b bar) bool {
	r := b.w / 2
	half := math.Max(0, b.h/2-r)
	dy := math.Max(0, math.Abs(y-b.cy)-half)
	dx := x - b.cx
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
		images[i] = encodePNG(render(s))
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

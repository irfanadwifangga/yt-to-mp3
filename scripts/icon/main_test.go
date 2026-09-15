package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"strings"
	"testing"
)

func TestBuildICO(t *testing.T) {
	data, err := buildICO(sizes)
	if err != nil {
		t.Fatalf("buildICO() error = %v", err)
	}

	r := bytes.NewReader(data)
	var header [3]uint16
	if err := binary.Read(r, binary.LittleEndian, &header); err != nil {
		t.Fatal(err)
	}
	if header[1] != 1 || int(header[2]) != len(sizes) {
		t.Fatalf("header = %v, mau tipe 1 dengan %d gambar", header, len(sizes))
	}

	for i, want := range sizes {
		var e icoEntry
		if err := binary.Read(r, binary.LittleEndian, &e); err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data[e.Offset : e.Offset+e.Size]))
		if err != nil {
			t.Fatalf("gambar %d bukan PNG: %v", i, err)
		}
		if b := img.Bounds(); b.Dx() != want || b.Dy() != want {
			t.Errorf("gambar %d = %dx%d, mau %d", i, b.Dx(), b.Dy(), want)
		}
	}
}

// Ikon terbaca sebagai sampul yang terisi dari bawah dengan gelombang suara
// di atasnya.
func TestRenderMengikutiTandaMerek(t *testing.T) {
	img := render(64)

	checks := []struct {
		name string
		x, y int
		want string
	}{
		{"sudut transparan", 0, 0, "transparan"},
		{"bar tengah", 32, 32, "wave"},
		{"sampul di atas bar", 32, 8, "cover"},
		{"isian hijau di bawah bar", 32, 56, "fill"},
		{"celah antarbar di atas batas isian", 24, 20, "cover"},
	}
	for _, c := range checks {
		got := img.NRGBAAt(c.x, c.y)
		var ok bool
		switch c.want {
		case "transparan":
			ok = got.A == 0
		case "wave":
			ok = got == wave
		case "cover":
			ok = got == cover
		case "fill":
			ok = got == fill
		}
		if !ok {
			t.Errorf("%s (%d,%d) = %v, mau %s", c.name, c.x, c.y, got, c.want)
		}
	}
}

// SVG harus memuat geometri yang sama: tile, dua bagian latar, dan satu
// rect per bar.
func TestBuildSVG(t *testing.T) {
	svg := buildSVG()
	if n := strings.Count(svg, "<rect"); n != 3+len(barHeights) {
		t.Errorf("jumlah rect = %d, mau %d", n, 3+len(barHeights))
	}
	for _, want := range []string{"#13233f", "#3b82f6", "#e8f1ff", `clip-path="url(#tile)"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("SVG tidak memuat %s", want)
		}
	}
}

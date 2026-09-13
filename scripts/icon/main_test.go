package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
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
		if e.Width != uint8(want%256) {
			t.Errorf("entri %d lebar = %d", i, e.Width)
		}
	}
}

// Tanda merek terbaca sebagai level meter: sudut transparan, bawah hijau,
// atas abu-abu.
func TestRenderMengikutiTandaMerek(t *testing.T) {
	img := render(64)

	if img.NRGBAAt(0, 0).A != 0 {
		t.Error("sudut seharusnya transparan")
	}
	if got := img.NRGBAAt(32, 56); got != signal {
		t.Errorf("bagian bawah = %v, mau %v", got, signal)
	}
	if got := img.NRGBAAt(32, 12); got != track {
		t.Errorf("bagian atas = %v, mau %v", got, track)
	}
}

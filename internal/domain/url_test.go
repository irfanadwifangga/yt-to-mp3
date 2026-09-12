package domain_test

import (
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

func TestNormalizeURL(t *testing.T) {
	const id = "dQw4w9WgXcQ"
	const key = "youtube:" + id

	// Seluruh bentuk di bawah menunjuk video yang sama dan wajib menghasilkan
	// kunci identik, karena source_key jadi dasar deteksi duplikat.
	valid := map[string]string{
		"watch biasa":          "https://www.youtube.com/watch?v=" + id,
		"tanpa www":            "https://youtube.com/watch?v=" + id,
		"http":                 "http://youtube.com/watch?v=" + id,
		"short link":           "https://youtu.be/" + id,
		"short link + query":   "https://youtu.be/" + id + "?t=42",
		"shorts":               "https://www.youtube.com/shorts/" + id,
		"embed":                "https://www.youtube.com/embed/" + id,
		"live":                 "https://www.youtube.com/live/" + id,
		"nocookie":             "https://www.youtube-nocookie.com/embed/" + id,
		"mobile":               "https://m.youtube.com/watch?v=" + id,
		"music":                "https://music.youtube.com/watch?v=" + id,
		"dengan playlist":      "https://www.youtube.com/watch?v=" + id + "&list=PL123&index=4",
		"dengan timestamp":     "https://www.youtube.com/watch?v=" + id + "&t=90s",
		"dengan tracking":      "https://youtu.be/" + id + "?si=AbCdEf",
		"host huruf besar":     "https://WWW.YouTube.COM/watch?v=" + id,
		"spasi di tepi":        "  https://youtu.be/" + id + "  ",
		"trailing slash embed": "https://www.youtube.com/embed/" + id + "/",
	}

	for name, raw := range valid {
		t.Run(name, func(t *testing.T) {
			got, err := domain.NormalizeURL(raw)
			if err != nil {
				t.Fatalf("NormalizeURL(%q) error = %v, mau nil", raw, err)
			}
			if got != key {
				t.Errorf("NormalizeURL(%q) = %q, mau %q", raw, got, key)
			}
		})
	}
}

func TestNormalizeURLDitolak(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want domain.ErrorCode
	}{
		{"kosong", "", domain.CodeInvalidURL},
		{"hanya spasi", "   ", domain.CodeInvalidURL},
		{"skema file", "file:///etc/passwd", domain.CodeInvalidURL},
		{"skema ftp", "ftp://youtube.com/watch?v=dQw4w9WgXcQ", domain.CodeInvalidURL},
		{"host lain", "https://vimeo.com/12345", domain.CodeUnsupportedURL},
		{"host mirip", "https://youtube.com.evil.test/watch?v=dQw4w9WgXcQ", domain.CodeUnsupportedURL},
		{"tanpa id", "https://www.youtube.com/", domain.CodeUnsupportedURL},
		{"halaman channel", "https://www.youtube.com/@someone", domain.CodeUnsupportedURL},
		{"id kependekan", "https://youtu.be/abc", domain.CodeUnsupportedURL},
		{"id kepanjangan", "https://youtu.be/dQw4w9WgXcQextra", domain.CodeUnsupportedURL},
		{"id karakter ilegal", "https://www.youtube.com/watch?v=dQw4w9WgXc$", domain.CodeUnsupportedURL},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := domain.NormalizeURL(tc.raw)
			if err == nil {
				t.Fatalf("NormalizeURL(%q) = %q, mau error", tc.raw, got)
			}
			if err.Code != tc.want {
				t.Errorf("kode = %q, mau %q (detail: %s)", err.Code, tc.want, err.Detail)
			}
			if err.Retryable() {
				t.Error("error URL tidak boleh retryable")
			}
		})
	}
}

func TestVideoID(t *testing.T) {
	if got := domain.VideoID("youtube:dQw4w9WgXcQ"); got != "dQw4w9WgXcQ" {
		t.Errorf("VideoID = %q, mau dQw4w9WgXcQ", got)
	}
}

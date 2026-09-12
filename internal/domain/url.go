package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// videoIDPattern adalah bentuk id video YouTube: 11 karakter base64url.
var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// supportedHosts memetakan host yang dikenali setelah dinormalkan.
var supportedHosts = map[string]bool{
	"youtube.com":          true,
	"m.youtube.com":        true,
	"music.youtube.com":    true,
	"youtube-nocookie.com": true,
	"youtu.be":             true,
}

// pathPrefixes adalah segmen path yang diikuti langsung oleh id video.
var pathPrefixes = []string{"shorts", "embed", "live", "v"}

// NormalizeURL mengubah berbagai bentuk URL YouTube menjadi satu kunci
// kanonik "youtube:<id>".
//
// Kunci ini dipakai untuk cache metadata dan deteksi duplikat, jadi seluruh
// parameter lain (list, t, si, parameter tracking) sengaja dibuang: dua URL
// yang menunjuk video sama harus menghasilkan kunci sama. Lihat docs planning
// "Normalisasi URL dan source_key".
func NormalizeURL(raw string) (string, *Error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", NewError(CodeInvalidURL, ClassPermanent, "URL kosong")
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return "", WrapError(CodeInvalidURL, ClassPermanent, "URL tidak dapat diparse", err)
	}

	// Skema di luar http/https ditolak sebelum menyentuh yt-dlp; file://
	// dan sejenisnya tidak boleh pernah sampai ke subprocess.
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", NewError(CodeInvalidURL, ClassPermanent,
			fmt.Sprintf("skema %q tidak diizinkan", u.Scheme))
	}

	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	if !supportedHosts[host] {
		return "", NewError(CodeUnsupportedURL, ClassPermanent,
			fmt.Sprintf("host %q tidak didukung", host))
	}

	id, ok := extractVideoID(host, u)
	if !ok {
		return "", NewError(CodeUnsupportedURL, ClassPermanent, "id video tidak ditemukan pada URL")
	}
	if !videoIDPattern.MatchString(id) {
		return "", NewError(CodeUnsupportedURL, ClassPermanent,
			fmt.Sprintf("id video %q tidak valid", id))
	}
	return "youtube:" + id, nil
}

// extractVideoID mengambil id dari bentuk URL yang dikenali.
func extractVideoID(host string, u *url.URL) (string, bool) {
	segments := splitPath(u.Path)

	if host == "youtu.be" {
		if len(segments) > 0 {
			return segments[0], true
		}
		return "", false
	}

	if v := u.Query().Get("v"); v != "" {
		return v, true
	}

	if len(segments) >= 2 {
		for _, prefix := range pathPrefixes {
			if segments[0] == prefix {
				return segments[1], true
			}
		}
	}
	return "", false
}

// splitPath memecah path menjadi segmen tanpa elemen kosong.
func splitPath(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// VideoID mengembalikan bagian id dari source key.
func VideoID(sourceKey string) string {
	_, id, _ := strings.Cut(sourceKey, ":")
	return id
}

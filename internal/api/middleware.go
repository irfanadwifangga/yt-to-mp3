package api

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// maxBodyBytes membatasi ukuran body request API.
const maxBodyBytes = 64 << 10

// recoverer mengubah panic jadi 500 tanpa menjatuhkan server.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic pada handler",
					"path", r.URL.Path,
					"panic", rec,
					"stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, domain.CodeInternal, "Terjadi kesalahan internal.")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeaders memasang header dasar untuk seluruh respons.
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// img-src membuka satu host saja, yaitu CDN sampul YouTube. Sampul itu
		// gambar yang sama dengan yang tersemat di MP3 hasil, dan mesin ini
		// memang sudah menghubungi YouTube lewat yt-dlp, jadi tidak ada pihak
		// baru yang mengetahui aktivitas pengguna. Referrer-Policy di atas
		// memastikan alamat aplikasi tidak ikut terkirim.
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: https://i.ytimg.com; "+
				"style-src 'self' 'unsafe-inline'; font-src 'self'; "+
				"connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// localOnly menolak request yang Host atau Origin-nya bukan milik kita.
//
// Ini mitigasi utama terhadap DNS rebinding dan CSRF: server di loopback tetap
// dapat dihubungi situs web mana pun yang sedang dibuka pengguna.
func (s *Server) localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !slices.Contains(s.allowedHosts, r.Host) {
			s.log.Warn("host ditolak", "host", r.Host, "path", r.URL.Path)
			writeError(w, http.StatusForbidden, CodeForbiddenHost, "Host tidak diizinkan.")
			return
		}

		// Origin hanya wajib untuk request yang mengubah state. Browser
		// mengirimkannya pada POST/PUT/DELETE same-origin.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			origin := r.Header.Get("Origin")
			if origin == "" || !slices.Contains(s.allowedOrigins, origin) {
				s.log.Warn("origin ditolak", "origin", origin, "path", r.URL.Path)
				writeError(w, http.StatusForbidden, CodeForbiddenOrigin, "Origin tidak diizinkan.")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

// bodyLimit membatasi ukuran body dan mewajibkan JSON bila ada isinya.
func (s *Server) bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

		if r.ContentLength > 0 {
			ct := r.Header.Get("Content-Type")
			if mediaType, _, _ := strings.Cut(ct, ";"); strings.TrimSpace(mediaType) != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, CodeUnsupportedMedia,
					"Body harus application/json.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireToken memeriksa session token dengan perbandingan konstan-waktu.
func (s *Server) requireToken(next http.Handler) http.Handler {
	want := []byte(s.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("X-Session-Token"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			writeError(w, http.StatusUnauthorized, CodeUnauthorized, "Session token tidak valid.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logRequests mencatat setiap request pada level debug.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.log.Enabled(r.Context(), slog.LevelDebug) {
			s.log.Debug("request", "method", r.Method, "path", r.URL.Path)
		}
		next.ServeHTTP(w, r)
	})
}

// chain menerapkan middleware dari luar ke dalam.
func chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

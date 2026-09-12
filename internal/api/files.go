package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// Revealer membuka lokasi berkas di file manager.
//
// Interface didefinisikan di sisi pemakai supaya api tidak perlu mengenal
// implementasinya.
type Revealer interface {
	Reveal(path string) error
}

// handleDownloadFile mengirim berkas hasil.
//
// Path tidak pernah datang dari klien: yang diterima hanya id berkas, dan
// path-nya dibaca dari database. Endpoint yang menerima path dari luar akan
// menjadi jalur membaca berkas sewenang-wenang di mesin pengguna.
func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	file, err := s.files.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	f, err := os.Open(file.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Berkas dihapus di luar aplikasi. Barisnya dipertahankan supaya
			// history tetap menunjukkan konversi pernah berhasil.
			if err := s.files.MarkMissing(r.Context(), file.ID); err != nil {
				s.log.Warn("tandai berkas hilang gagal", "file", file.ID, "error", err)
			}
			writeError(w, http.StatusNotFound, domain.CodeJobNotFound,
				"Berkas sudah tidak ada di disk.")
			return
		}
		s.log.Error("buka berkas gagal", "file", file.ID, "error", err)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Berkas tidak dapat dibuka.")
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Berkas tidak dapat dibaca.")
		return
	}

	w.Header().Set("Content-Type", file.MIME)
	w.Header().Set("Content-Disposition", contentDisposition(file.Filename))
	http.ServeContent(w, r, file.Filename, info.ModTime(), f)
}

// contentDisposition menyusun header unduhan.
//
// Nama disandikan sebagai filename* (RFC 5987) karena judul non-ASCII
// seperti aksara Jepang tidak dapat diwakili header latin-1.
func contentDisposition(name string) string {
	return `attachment; filename="` + asciiFallback(name) + `"; filename*=UTF-8''` + pathEscape(name)
}

// asciiFallback menyediakan nama sederhana untuk klien lama.
func asciiFallback(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			out = append(out, '_')
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		return "audio"
	}
	return string(out)
}

// pathEscape menyandikan nama untuk bagian filename*.
func pathEscape(name string) string {
	const safe = "!#$&+-.^_`|~"
	var b []byte
	for _, c := range []byte(name) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			containsByte(safe, c) {
			b = append(b, c)
			continue
		}
		b = append(b, '%', hexDigit(c>>4), hexDigit(c&0x0f))
	}
	return string(b)
}

func containsByte(s string, c byte) bool {
	for i := range len(s) {
		if s[i] == c {
			return true
		}
	}
	return false
}

func hexDigit(n byte) byte {
	if n < 10 {
		return '0' + n
	}
	return 'A' + (n - 10)
}

// handleRevealFile membuka lokasi berkas di file manager.
func (s *Server) handleRevealFile(w http.ResponseWriter, r *http.Request) {
	file, err := s.files.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeDomainError(w, err)
		return
	}

	if _, err := os.Stat(file.Path); err != nil {
		writeError(w, http.StatusNotFound, domain.CodeJobNotFound,
			"Berkas sudah tidak ada di disk.")
		return
	}

	if s.revealer == nil {
		writeError(w, http.StatusServiceUnavailable, domain.CodeInternal,
			"Membuka lokasi berkas tidak didukung.")
		return
	}
	if err := s.revealer.Reveal(filepath.Clean(file.Path)); err != nil {
		s.log.Warn("reveal gagal", "file", file.ID, "error", err)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Tidak dapat membuka lokasi berkas.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

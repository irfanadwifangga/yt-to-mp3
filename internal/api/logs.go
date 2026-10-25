package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

type logsResponse struct {
	Lines []string `json:"lines"`
	Path  string   `json:"path"`
}

// handleGetLogs mengembalikan baris-baris terakhir dari app.log.
func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	logPath := filepath.Join(s.cfg.Paths.LogDir, "app.log")
	linesLimit := 200
	if raw := r.URL.Query().Get("lines"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > 1000 {
				n = 1000
			}
			linesLimit = n
		}
	}

	lines, err := readLastLines(logPath, linesLimit)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		s.log.Warn("baca berkas log gagal", "path", logPath, "error", err)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal, "Tidak dapat membaca berkas log.")
		return
	}
	if lines == nil {
		lines = []string{}
	}

	writeJSON(w, http.StatusOK, logsResponse{
		Lines: lines,
		Path:  logPath,
	})
}

// handleRevealLogs membuka berkas atau direktori log di file manager.
func (s *Server) handleRevealLogs(w http.ResponseWriter, _ *http.Request) {
	logPath := filepath.Join(s.cfg.Paths.LogDir, "app.log")
	if _, err := os.Stat(logPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logPath = s.cfg.Paths.LogDir
		}
	}

	if s.revealer == nil {
		writeError(w, http.StatusServiceUnavailable, domain.CodeInternal,
			"Membuka lokasi berkas tidak didukung.")
		return
	}
	if err := s.revealer.Reveal(filepath.Clean(logPath)); err != nil {
		s.log.Warn("reveal log gagal", "path", logPath, "error", err)
		writeError(w, http.StatusInternalServerError, domain.CodeInternal,
			"Tidak dapat membuka lokasi berkas log.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readLastLines(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if stat.Size() == 0 {
		return []string{}, nil
	}

	const maxReadBytes = 1024 * 1024 // 1 MB
	var offset int64
	readLen := stat.Size()
	if readLen > maxReadBytes {
		offset = stat.Size() - maxReadBytes
		readLen = maxReadBytes
	}

	buf := make([]byte, readLen)
	if _, err := f.ReadAt(buf, offset); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	raw := string(buf)
	if offset > 0 {
		if idx := strings.IndexByte(raw, '\n'); idx >= 0 {
			raw = raw[idx+1:]
		}
	}

	all := strings.Split(raw, "\n")
	var lines []string
	for _, l := range all {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			lines = append(lines, l)
		}
	}

	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines, nil
}

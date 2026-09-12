// Package api menyediakan HTTP server lokal beserta middleware keamanannya.
//
// Server hanya listen di loopback. Seluruh endpoint /api kecuali /api/ping
// mewajibkan session token. Lihat docs planning "Model keamanan lokal".
package api

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
)

// Options adalah dependensi yang disuntikkan ke Server.
type Options struct {
	Config   config.Config
	Logger   *slog.Logger
	Token    string
	Port     int
	SPA      fs.FS
	SPABuilt bool
	Dev      bool

	Tools    application.ToolManager
	Metadata *application.MetadataService
	Presets  application.PresetLister
}

// Server membungkus router beserta seluruh state HTTP.
type Server struct {
	handler http.Handler
	cfg     config.Config
	log     *slog.Logger
	token   string

	allowedHosts   []string
	allowedOrigins []string

	spa      fs.FS
	spaBuilt bool

	tools    application.ToolManager
	metadata *application.MetadataService
	presets  application.PresetLister

	startedAt time.Time

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
}

// NewToken membuat session token acak 32 byte.
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("buat token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// New menyusun server beserta rute dan middleware-nya.
func New(opts Options) *Server {
	s := &Server{
		cfg:        opts.Config,
		log:        opts.Logger,
		token:      opts.Token,
		spa:        opts.SPA,
		spaBuilt:   opts.SPABuilt,
		tools:      opts.Tools,
		metadata:   opts.Metadata,
		presets:    opts.Presets,
		startedAt:  time.Now(),
		shutdownCh: make(chan struct{}),
	}

	s.allowedHosts = []string{
		fmt.Sprintf("127.0.0.1:%d", opts.Port),
		fmt.Sprintf("localhost:%d", opts.Port),
	}
	s.allowedOrigins = []string{
		fmt.Sprintf("http://127.0.0.1:%d", opts.Port),
		fmt.Sprintf("http://localhost:%d", opts.Port),
	}

	// Mode dev menjalankan SPA di Vite dev server pada origin berbeda,
	// sehingga origin itu harus diizinkan secara eksplisit. Tidak pernah
	// aktif pada build rilis.
	if opts.Dev {
		s.allowedHosts = append(s.allowedHosts, "127.0.0.1:5173", "localhost:5173")
		s.allowedOrigins = append(s.allowedOrigins, "http://127.0.0.1:5173", "http://localhost:5173")
		s.log.Warn("mode dev aktif: origin Vite diizinkan")
	}

	protected := http.NewServeMux()
	protected.HandleFunc("GET /health", s.handleHealth)
	protected.HandleFunc("POST /shutdown", s.handleShutdown)
	protected.HandleFunc("GET /tools", s.handleTools)
	protected.HandleFunc("POST /tools/install", s.handleToolInstall)
	protected.HandleFunc("POST /metadata", s.handleMetadata)
	protected.HandleFunc("GET /presets", s.handlePresets)

	root := http.NewServeMux()
	root.HandleFunc("GET /api/ping", s.handlePing)
	root.Handle("/api/", http.StripPrefix("/api", s.requireToken(protected)))
	root.Handle("/", s.spaHandler())

	s.handler = chain(root,
		s.recoverer,
		s.logRequests,
		s.securityHeaders,
		s.localOnly,
		s.bodyLimit,
	)
	return s
}

// ServeHTTP membuat Server memenuhi http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// ShutdownRequested ditutup ketika POST /api/shutdown dipanggil.
func (s *Server) ShutdownRequested() <-chan struct{} {
	return s.shutdownCh
}

func (s *Server) requestShutdown() {
	s.shutdownOnce.Do(func() {
		s.log.Info("shutdown diminta lewat API")
		close(s.shutdownCh)
	})
}

// spaHandler menyajikan SPA hasil embed, dengan fallback ke index.html untuk
// rute sisi klien. Bila SPA belum pernah di-build, sajikan halaman petunjuk
// alih-alih 404 yang membingungkan.
func (s *Server) spaHandler() http.Handler {
	if !s.spaBuilt {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				writeError(w, http.StatusNotFound, CodeNotFound, "Tidak ditemukan.")
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(placeholderHTML))
		})
	}

	files := http.FileServerFS(s.spa)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "" || clean == "." {
			clean = "index.html"
		}
		if _, err := fs.Stat(s.spa, clean); err != nil {
			// Rute milik router sisi klien: kembalikan shell SPA.
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

const placeholderHTML = `<!doctype html>
<html lang="id">
<head><meta charset="utf-8"><title>yt-to-mp3</title>
<style>
 body{font:15px/1.6 system-ui,sans-serif;margin:0;display:grid;place-items:center;
      min-height:100vh;background:#11131a;color:#e6e8ef}
 main{max-width:34rem;padding:2rem}
 code{background:#1e2230;padding:.15rem .4rem;border-radius:4px}
 h1{font-size:1.25rem;margin:0 0 .75rem}
 p{color:#9aa3b8}
</style></head>
<body><main>
 <h1>Server berjalan, SPA belum di-build</h1>
 <p>Backend sudah siap dan <code>/api/health</code> dapat diakses. Frontend
 belum pernah di-build, jadi halaman ini yang tampil.</p>
 <p>Jalankan <code>make build-web</code> (atau <code>npm --prefix web install
 &amp;&amp; npm --prefix web run build</code>), lalu build ulang binary-nya.</p>
</main></body></html>`

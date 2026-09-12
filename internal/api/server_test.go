package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
)

const (
	testToken = "token-untuk-test"
	testPort  = 8080
	testHost  = "127.0.0.1:8080"
	testOrig  = "http://127.0.0.1:8080"
)

func newServer(t *testing.T) *api.Server {
	t.Helper()
	return api.New(api.Options{
		Config:   config.Default(),
		Logger:   newDiscardLogger(),
		Token:    testToken,
		Port:     testPort,
		SPA:      fstest.MapFS{},
		SPABuilt: false,
	})
}

// Kontrol keamanan di sini adalah mitigasi CSRF dan DNS rebinding untuk
// server loopback. Regresinya sulit terlihat manual, jadi dikunci test.
func TestSecurityMiddleware(t *testing.T) {
	srv := newServer(t)

	tests := []struct {
		name   string
		method string
		path   string
		host   string
		origin string
		token  string
		want   int
	}{
		{"ping publik tanpa token", http.MethodGet, "/api/ping", testHost, "", "", http.StatusOK},
		{"health tanpa token ditolak", http.MethodGet, "/api/health", testHost, "", "", http.StatusUnauthorized},
		{"health token salah ditolak", http.MethodGet, "/api/health", testHost, "", "salah", http.StatusUnauthorized},
		{"health token benar", http.MethodGet, "/api/health", testHost, "", testToken, http.StatusOK},
		{"host asing ditolak", http.MethodGet, "/api/ping", "evil.example.com", "", "", http.StatusForbidden},
		{"host localhost diterima", http.MethodGet, "/api/ping", "localhost:8080", "", "", http.StatusOK},
		{"post tanpa origin ditolak", http.MethodPost, "/api/shutdown", testHost, "", testToken, http.StatusForbidden},
		{"post origin asing ditolak", http.MethodPost, "/api/shutdown", testHost, "http://evil.test", testToken, http.StatusForbidden},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.token != "" {
				req.Header.Set("X-Session-Token", tc.token)
			}

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, mau %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestShutdownSignalsChannel(t *testing.T) {
	srv := newServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/shutdown", nil)
	req.Host = testHost
	req.Header.Set("Origin", testOrig)
	req.Header.Set("X-Session-Token", testToken)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusAccepted)
	}
	select {
	case <-srv.ShutdownRequested():
	default:
		t.Fatal("kanal shutdown tidak ditutup")
	}
}

func TestSecurityHeadersSelaluAda(t *testing.T) {
	srv := newServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Host = testHost
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, mau nosniff", got)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy kosong")
	}
}

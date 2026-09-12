package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

const (
	testToken = "token-untuk-test"
	testPort  = 8080
	testHost  = "127.0.0.1:8080"
	testOrig  = "http://127.0.0.1:8080"
)

type harness struct {
	srv      *api.Server
	tools    *fakeTools
	resolver *fakeResolver
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ft := newFakeTools()
	fr := newFakeResolver()
	return &harness{
		srv: api.New(api.Options{
			Config:   config.Default(),
			Logger:   newDiscardLogger(),
			Token:    testToken,
			Port:     testPort,
			SPA:      fstest.MapFS{},
			SPABuilt: false,
			Tools:    ft,
			Resolver: fr,
		}),
		tools:    ft,
		resolver: fr,
	}
}

// do mengirim request dengan header yang sudah benar secara default.
func (h *harness) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Host = testHost
	r.Header.Set("X-Session-Token", testToken)
	if method != http.MethodGet {
		r.Header.Set("Origin", testOrig)
	}
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, r)
	return rec
}

// codeOf mengambil error.code dari body respons.
func codeOf(t *testing.T, rec *httptest.ResponseRecorder) domain.ErrorCode {
	t.Helper()
	var body struct {
		Error struct {
			Code domain.ErrorCode `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body bukan JSON error: %s", rec.Body.String())
	}
	return body.Error.Code
}

// Kontrol keamanan di sini adalah mitigasi CSRF dan DNS rebinding untuk
// server loopback. Regresinya sulit terlihat manual, jadi dikunci test.
func TestSecurityMiddleware(t *testing.T) {
	h := newHarness(t)

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
			h.srv.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("status = %d, mau %d (body: %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestShutdownSignalsChannel(t *testing.T) {
	h := newHarness(t)

	rec := h.do(t, http.MethodPost, "/api/shutdown", "")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusAccepted)
	}
	select {
	case <-h.srv.ShutdownRequested():
	default:
		t.Fatal("kanal shutdown tidak ditutup")
	}
}

func TestSecurityHeadersSelaluAda(t *testing.T) {
	h := newHarness(t)

	rec := h.do(t, http.MethodGet, "/api/ping", "")
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, mau nosniff", got)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy kosong")
	}
}

func TestHealthMelaporkanStatusTool(t *testing.T) {
	h := newHarness(t)

	rec := h.do(t, http.MethodGet, "/api/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", rec.Code)
	}

	var body struct {
		Tools map[string]struct {
			Available bool   `json:"available"`
			Version   string `json:"version"`
			Path      string `json:"path"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !body.Tools["yt-dlp"].Available {
		t.Error("yt-dlp seharusnya dilaporkan tersedia")
	}
	if body.Tools["ffmpeg"].Available {
		t.Error("ffmpeg seharusnya dilaporkan belum tersedia")
	}
	// Path filesystem tidak boleh pernah menyeberang batas API.
	if strings.Contains(rec.Body.String(), `"path"`) {
		t.Error("respons memuat path filesystem")
	}
}

func TestMetadataMenolakURLTidakValid(t *testing.T) {
	tests := []struct {
		name string
		body string
		want domain.ErrorCode
	}{
		{"skema file", `{"url":"file:///etc/passwd"}`, domain.CodeInvalidURL},
		{"host lain", `{"url":"https://vimeo.com/12345"}`, domain.CodeUnsupportedURL},
		{"url kosong", `{"url":""}`, domain.CodeInvalidURL},
		{"json rusak", `{"url":`, "BAD_REQUEST"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			rec := h.do(t, http.MethodPost, "/api/metadata", tc.body)

			if rec.Code < 400 {
				t.Fatalf("status = %d, mau >= 400", rec.Code)
			}
			if got := codeOf(t, rec); got != tc.want {
				t.Errorf("kode = %s, mau %s", got, tc.want)
			}
			// Validasi wajib terjadi sebelum resolver dipanggil, supaya
			// input berbahaya tidak pernah sampai ke subprocess.
			if h.resolver.calls != 0 {
				t.Errorf("resolver dipanggil %d kali, mau 0", h.resolver.calls)
			}
		})
	}
}

func TestMetadataMenormalkanURL(t *testing.T) {
	h := newHarness(t)

	body := `{"url":"https://youtu.be/dQw4w9WgXcQ?si=abc&t=30"}`
	rec := h.do(t, http.MethodPost, "/api/metadata", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200 (body: %s)", rec.Code, rec.Body.String())
	}

	if h.resolver.lastKey != "youtube:dQw4w9WgXcQ" {
		t.Errorf("resolver menerima %q, mau youtube:dQw4w9WgXcQ", h.resolver.lastKey)
	}

	var got struct {
		Title      string `json:"title"`
		SampleRate int    `json:"sample_rate"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Title != "Judul Contoh" {
		t.Errorf("title = %q", got.Title)
	}
	if got.SampleRate != 48000 {
		t.Errorf("sample_rate = %d, mau 48000", got.SampleRate)
	}
}

// Livestream ditolak dengan kode tersendiri dan status 422, bukan 500.
func TestMetadataLivestreamDitolak(t *testing.T) {
	h := newHarness(t)
	h.resolver.err = domain.NewError(domain.CodeLiveNotSupported, domain.ClassPermanent,
		"sumber adalah siaran langsung")

	rec := h.do(t, http.MethodPost, "/api/metadata",
		`{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, mau 422", rec.Code)
	}
	if got := codeOf(t, rec); got != domain.CodeLiveNotSupported {
		t.Errorf("kode = %s, mau %s", got, domain.CodeLiveNotSupported)
	}
}

func TestToolInstall(t *testing.T) {
	t.Run("nama tidak dikenal ditolak", func(t *testing.T) {
		h := newHarness(t)
		rec := h.do(t, http.MethodPost, "/api/tools/install", `{"name":"rm -rf"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, mau 400", rec.Code)
		}
		if len(h.tools.installed) != 0 {
			t.Error("tool tidak dikenal tidak boleh dipasang")
		}
	})

	t.Run("manifest belum di-pin dilaporkan 503", func(t *testing.T) {
		h := newHarness(t)
		h.tools.installErr = domain.NewError(domain.CodeToolManifest, domain.ClassLocal,
			"belum di-pin")

		rec := h.do(t, http.MethodPost, "/api/tools/install", `{"name":"ffmpeg"}`)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, mau 503", rec.Code)
		}
		if got := codeOf(t, rec); got != domain.CodeToolManifest {
			t.Errorf("kode = %s, mau %s", got, domain.CodeToolManifest)
		}
	})

	t.Run("berhasil", func(t *testing.T) {
		h := newHarness(t)
		rec := h.do(t, http.MethodPost, "/api/tools/install", `{"name":"ffmpeg"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, mau 200 (body: %s)", rec.Code, rec.Body.String())
		}
		if len(h.tools.installed) != 1 || h.tools.installed[0] != "ffmpeg" {
			t.Errorf("installed = %v, mau [ffmpeg]", h.tools.installed)
		}
	})
}

// Body non-JSON ditolak middleware sebelum mencapai handler; ini yang
// memaksa preflight untuk request lintas origin.
func TestBodyHarusJSON(t *testing.T) {
	h := newHarness(t)

	req := httptest.NewRequest(http.MethodPost, "/api/metadata",
		strings.NewReader("url=https://youtu.be/dQw4w9WgXcQ"))
	req.Host = testHost
	req.Header.Set("Origin", testOrig)
	req.Header.Set("X-Session-Token", testToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, mau 415", rec.Code)
	}
}

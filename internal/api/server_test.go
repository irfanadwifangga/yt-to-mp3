package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
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
	srv       *api.Server
	tools     *fakeTools
	resolver  *fakeResolver
	cache     *fakeCache
	presets   *fakePresets
	jobs      *fakeJobRepo
	canceller *fakeCanceller
	files     *fakeFiles
	revealer  *fakeRevealer
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ft := newFakeTools()
	fr := newFakeResolver()
	fc := newFakeCache()
	fp := newFakePresets()
	jr := newFakeJobRepo()
	fcan := &fakeCanceller{}
	ff := newFakeFiles()
	frev := &fakeRevealer{}
	log := newDiscardLogger()

	hub := api.NewHub(jr, log)
	live := func() application.LiveSettings {
		return application.LiveSettings{
			DefaultPresetID: "mp3_standard",
			FilenameMode:    domain.FilenameTitle,
			MaxQueueDepth:   3,
		}
	}
	jobService := application.NewJobService(jr, fp, fc, fr, ff, hub, nil, live, log)

	return &harness{
		srv: api.New(api.Options{
			Config:    config.Default(),
			Logger:    log,
			Token:     testToken,
			Port:      testPort,
			SPA:       fstest.MapFS{},
			SPABuilt:  false,
			Tools:     ft,
			Presets:   fp,
			Metadata:  application.NewMetadataService(fr, fc, log),
			Jobs:      jobService,
			Canceller: fcan,
			Hub:       hub,
			Files:     ff,
			Revealer:  frev,
		}),
		tools:     ft,
		resolver:  fr,
		cache:     fc,
		presets:   fp,
		jobs:      jr,
		canceller: fcan,
		files:     ff,
		revealer:  frev,
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

// Cache yang mengena harus mencegah pemanggilan yt-dlp sama sekali; itu
// seluruh alasan keberadaannya.
func TestMetadataMemakaiCache(t *testing.T) {
	h := newHarness(t)
	body := `{"url":"https://www.youtube.com/watch?v=dQw4w9WgXcQ"}`

	if rec := h.do(t, http.MethodPost, "/api/metadata", body); rec.Code != http.StatusOK {
		t.Fatalf("panggilan pertama status = %d", rec.Code)
	}
	if h.resolver.calls != 1 {
		t.Fatalf("resolver dipanggil %d kali, mau 1", h.resolver.calls)
	}
	if h.cache.puts != 1 {
		t.Errorf("cache ditulis %d kali, mau 1", h.cache.puts)
	}

	if rec := h.do(t, http.MethodPost, "/api/metadata", body); rec.Code != http.StatusOK {
		t.Fatalf("panggilan kedua status = %d", rec.Code)
	}
	if h.resolver.calls != 1 {
		t.Errorf("resolver dipanggil %d kali setelah cache terisi, mau tetap 1", h.resolver.calls)
	}
}

func TestPresets(t *testing.T) {
	h := newHarness(t)

	rec := h.do(t, http.MethodGet, "/api/presets", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var body struct {
		Presets []struct {
			ID         string `json:"id"`
			Kind       string `json:"kind"`
			SampleRate int    `json:"sample_rate"`
			MaxHeight  int    `json:"max_height"`
		} `json:"presets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Presets) != 2 || body.Presets[0].ID != "mp3_standard" {
		t.Fatalf("preset = %+v", body.Presets)
	}
	// UI mengelompokkan preset menurut kind dan menandai resolusi yang
	// melebihi sumber lewat max_height; keduanya bagian kontrak.
	if body.Presets[0].Kind != "audio" || body.Presets[1].Kind != "video" || body.Presets[1].MaxHeight != 720 {
		t.Errorf("kind/max_height = %+v", body.Presets)
	}
	if body.Presets[0].SampleRate != 48000 {
		t.Errorf("sample_rate = %d, mau 48000", body.Presets[0].SampleRate)
	}
}

func TestToolCheckDanUpdate(t *testing.T) {
	t.Run("cek berhasil membawa waktu cek", func(t *testing.T) {
		h := newHarness(t)
		rec := h.do(t, http.MethodPost, "/api/tools/check", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, mau 200 (body: %s)", rec.Code, rec.Body.String())
		}
		var body struct {
			CheckedAt string `json:"checked_at"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if h.tools.checks != 1 || body.CheckedAt == "" {
			t.Errorf("checks = %d, checked_at = %q", h.tools.checks, body.CheckedAt)
		}
	})

	t.Run("cek gagal dilaporkan 503", func(t *testing.T) {
		h := newHarness(t)
		h.tools.checkErr = domain.NewError(domain.CodeToolUpdateCheck, domain.ClassTransient, "offline")
		rec := h.do(t, http.MethodPost, "/api/tools/check", "")
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, mau 503", rec.Code)
		}
		if got := codeOf(t, rec); got != domain.CodeToolUpdateCheck {
			t.Errorf("kode = %s, mau %s", got, domain.CodeToolUpdateCheck)
		}
	})

	// FFmpeg mengikuti manifest ter-pin; hanya yt-dlp yang boleh diperbarui
	// dari checksum rilis hulunya.
	t.Run("hanya yt-dlp yang dapat diperbarui", func(t *testing.T) {
		h := newHarness(t)
		for _, name := range []string{"ffmpeg", "rm -rf"} {
			rec := h.do(t, http.MethodPost, "/api/tools/update", `{"name":"`+name+`"}`)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, mau 400", name, rec.Code)
			}
		}
		if len(h.tools.updated) != 0 {
			t.Errorf("updated = %v, mau kosong", h.tools.updated)
		}

		rec := h.do(t, http.MethodPost, "/api/tools/update", `{"name":"yt-dlp"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, mau 200 (body: %s)", rec.Code, rec.Body.String())
		}
		if len(h.tools.updated) != 1 || h.tools.updated[0] != "yt-dlp" {
			t.Errorf("updated = %v, mau [yt-dlp]", h.tools.updated)
		}
	})
}

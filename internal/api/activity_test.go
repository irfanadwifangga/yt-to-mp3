package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
)

// Hanya request bertoken yang dihitung sebagai pemakaian. Situs lain dapat
// menembak 127.0.0.1 tanpa token, dan itu tidak boleh menunda idle shutdown.
func TestAktivitasHanyaDariRequestTerautentikasi(t *testing.T) {
	srv := api.New(api.Options{
		Config: config.Default(),
		Logger: newDiscardLogger(),
		Token:  testToken,
		Port:   testPort,
		SPA:    fstest.MapFS{},
	})

	send := func(path string, withToken bool) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = testHost
		if withToken {
			req.Header.Set("X-Session-Token", testToken)
		}
		srv.ServeHTTP(httptest.NewRecorder(), req)
	}

	start := srv.LastActivity()
	time.Sleep(5 * time.Millisecond)

	send("/api/presets", false) // ditolak 401
	send("/api/ping", false)    // publik
	if got := srv.LastActivity(); !got.Equal(start) {
		t.Errorf("request tanpa token mengubah aktivitas: %v -> %v", start, got)
	}

	send("/api/settings", true)
	if got := srv.LastActivity(); !got.After(start) {
		t.Errorf("request bertoken tidak tercatat: %v -> %v", start, got)
	}
}

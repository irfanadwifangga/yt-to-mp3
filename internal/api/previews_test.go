package api_test

import (
	"context"
	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
	"github.com/irfanadwifangga/yt-to-mp3/internal/preview"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type previewBuilderFake struct{}

func (previewBuilderFake) Build(_ context.Context, _ string, dir string, _ func(string, *float64)) error {
	return os.WriteFile(filepath.Join(dir, "preview.mp4"), []byte("0123456789"), 0600)
}
func TestPreviewMediaAuthenticationRangesAndDeletion(t *testing.T) {
	m, e := preview.New(context.Background(), t.TempDir(), previewBuilderFake{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	s, e := m.Create("https://youtu.be/aaaaaaaaaaa")
	if e != nil {
		t.Fatal(e)
	}
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		state, _ := m.Get(s.ID)
		if state.State == "ready" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	h := newHarness(t)
	h.srv = api.New(api.Options{Config: config.Default(), Logger: newDiscardLogger(), Token: testToken, Port: testPort, Previews: m})
	path := "/api/previews/" + s.ID + "/media"
	request := httptest.NewRequest(http.MethodGet, "http://"+testHost+path, nil)
	denied := httptest.NewRecorder()
	h.srv.ServeHTTP(denied, request)
	if denied.Code != 401 {
		t.Fatalf("unauthorized media returned %d", denied.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "http://"+testHost+path, nil)
	request.Header.Set("X-Session-Token", testToken)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	h.srv.ServeHTTP(response, request)
	if response.Code != 206 || response.Body.String() != "2345" || response.Header().Get("Content-Type") != "video/mp4" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("bad range response: %d %s", response.Code, response.Body.String())
	}
	deleted := h.do(t, http.MethodDelete, "/api/previews/"+s.ID, "")
	if deleted.Code != 204 {
		t.Fatal(deleted.Code)
	}
	missing := h.do(t, http.MethodGet, path, "")
	if missing.Code != 404 {
		t.Fatal("deleted preview remained readable")
	}
}

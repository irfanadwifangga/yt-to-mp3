package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/irfanadwifangga/yt-to-mp3/internal/api"
	"github.com/irfanadwifangga/yt-to-mp3/internal/config"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

type fakePicker struct {
	path  string
	ok    bool
	err   error
	title string
	start string
	calls int
}

func (p *fakePicker) PickFolder(_ context.Context, title, start string) (string, bool, error) {
	p.calls++
	p.title = title
	p.start = start
	return p.path, p.ok, p.err
}

func newDialogServer(picker api.FolderPicker) *api.Server {
	return api.New(api.Options{
		Config: config.Default(),
		Logger: newDiscardLogger(),
		Token:  testToken,
		Port:   testPort,
		SPA:    fstest.MapFS{},
		Picker: picker,
	})
}

func pickFolder(srv *api.Server, body string, mutate func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/dialogs/folder", strings.NewReader(body))
	req.Host = testHost
	req.Header.Set("Origin", testOrig)
	req.Header.Set("X-Session-Token", testToken)
	req.Header.Set("Content-Type", "application/json")
	if mutate != nil {
		mutate(req)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

type folderBody struct {
	Path      string `json:"path"`
	Cancelled bool   `json:"cancelled"`
}

func TestPilihFolderMengembalikanPath(t *testing.T) {
	picker := &fakePicker{path: `C:\Users\Nama\Music`, ok: true}
	rec := pickFolder(newDialogServer(picker), `{"title":"Pilih folder","start":"C:\\Users"}`, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200 (body: %s)", rec.Code, rec.Body.String())
	}
	var body folderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Path != picker.path || body.Cancelled {
		t.Errorf("respons = %+v", body)
	}
	if picker.title != "Pilih folder" || picker.start != `C:\Users` {
		t.Errorf("picker menerima title=%q start=%q", picker.title, picker.start)
	}
}

func TestPilihFolderDibatalkan(t *testing.T) {
	rec := pickFolder(newDialogServer(&fakePicker{ok: false}), `{}`, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau 200", rec.Code)
	}
	var body folderBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Cancelled || body.Path != "" {
		t.Errorf("respons = %+v, mau dibatalkan tanpa path", body)
	}
}

// Tanpa pemilih native, UI harus diberi tahu dengan kode yang jelas supaya
// bisa jatuh ke input manual, bukan menerima INTERNAL.
func TestPilihFolderTidakTersedia(t *testing.T) {
	tests := map[string]api.FolderPicker{
		"picker gagal": &fakePicker{err: errors.New("zenity tidak ditemukan")},
		"tanpa picker": nil,
	}
	for name, picker := range tests {
		t.Run(name, func(t *testing.T) {
			rec := pickFolder(newDialogServer(picker), `{}`, nil)
			if rec.Code != http.StatusServiceUnavailable {
				t.Errorf("status = %d, mau 503", rec.Code)
			}
			if got := codeOf(t, rec); got != domain.CodeDialogUnavailable {
				t.Errorf("kode = %s, mau %s", got, domain.CodeDialogUnavailable)
			}
		})
	}
}

// Dialog muncul di layar pengguna, jadi situs web lain tidak boleh bisa
// memicunya: token dan Origin tetap wajib.
func TestPilihFolderDijagaTokenDanOrigin(t *testing.T) {
	tests := map[string]struct {
		mutate func(*http.Request)
		want   int
	}{
		"tanpa token":  {func(r *http.Request) { r.Header.Del("X-Session-Token") }, http.StatusUnauthorized},
		"origin asing": {func(r *http.Request) { r.Header.Set("Origin", "http://evil.test") }, http.StatusForbidden},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			picker := &fakePicker{path: "C:\\x", ok: true}
			rec := pickFolder(newDialogServer(picker), `{}`, tc.mutate)

			if rec.Code != tc.want {
				t.Errorf("status = %d, mau %d", rec.Code, tc.want)
			}
			if picker.calls != 0 {
				t.Errorf("dialog terbuka %d kali, mau 0", picker.calls)
			}
		})
	}
}

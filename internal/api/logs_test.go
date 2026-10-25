package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestGetLogs(t *testing.T) {
	h := newHarness(t)

	// Saat berkas belum ada
	rec := h.do(t, http.MethodGet, "/api/logs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusOK)
	}

	var res struct {
		Lines []string `json:"lines"`
		Path  string   `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 0 {
		t.Errorf("lines = %v, mau kosong saat berkas belum ada", res.Lines)
	}

	// Tulis log contoh
	_ = os.MkdirAll(filepath.Dir(res.Path), 0o755)
	logContent := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	if err := os.WriteFile(res.Path, []byte(logContent), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(res.Path)

	// Ambil semua (default limit 200)
	rec = h.do(t, http.MethodGet, "/api/logs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusOK)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 5 {
		t.Errorf("len(lines) = %d, mau 5", len(res.Lines))
	}
	if res.Lines[0] != "line 1" || res.Lines[4] != "line 5" {
		t.Errorf("lines salah: %v", res.Lines)
	}

	// Ambil dengan limit lines=2
	rec = h.do(t, http.MethodGet, "/api/logs?lines=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusOK)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Lines) != 2 {
		t.Errorf("len(lines) = %d, mau 2", len(res.Lines))
	}
	if res.Lines[0] != "line 4" || res.Lines[1] != "line 5" {
		t.Errorf("lines salah: %v", res.Lines)
	}
}

func TestRevealLogs(t *testing.T) {
	h := newHarness(t)
	rec := h.do(t, http.MethodPost, "/api/logs/reveal", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, mau %d", rec.Code, http.StatusNoContent)
	}
	if len(h.revealer.revealed) != 1 {
		t.Errorf("revealed = %v, mau 1 panggilan", h.revealer.revealed)
	}
}

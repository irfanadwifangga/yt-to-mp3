package instance_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/instance"
	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
)

// pingServer meniru /api/ping sebuah instance yang sedang berjalan.
func pingServer(t *testing.T, app string) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ping" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"app": app})
	}))
	t.Cleanup(srv.Close)
	return portOf(t, srv.Listener.Addr())
}

func portOf(t *testing.T, addr net.Addr) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func writeInfo(t *testing.T, port int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.json")
	err := instance.Write(path, instance.Info{
		PID: 1234, Port: port, Token: "rahasia", StartedAt: time.Now().UTC(), Version: "test",
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	return path
}

// Launch kedua harus menemukan instance pertama dan membuka jendelanya,
// bukan menjalankan server baru.
func TestFindRunningMenemukanInstanceHidup(t *testing.T) {
	port := pingServer(t, version.AppName)
	path := writeInfo(t, port)

	info, ok := instance.FindRunning(path)
	if !ok {
		t.Fatal("instance yang hidup tidak ditemukan")
	}
	if info.Port != port || info.Token != "rahasia" {
		t.Errorf("info = %+v", info)
	}
	if want := "http://127.0.0.1:" + strconv.Itoa(port) + "/?token=rahasia"; info.URL() != want {
		t.Errorf("URL() = %q, mau %q", info.URL(), want)
	}
}

func TestFindRunningMengabaikanYangTidakHidup(t *testing.T) {
	// Port yang pernah dipakai lalu ditutup: sisa proses yang mati mendadak.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadPort := portOf(t, ln.Addr())
	_ = ln.Close()

	corrupt := filepath.Join(t.TempDir(), "runtime.json")
	if err := os.WriteFile(corrupt, []byte("{bukan json"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		"berkas basi":                writeInfo(t, deadPort),
		"port dipakai aplikasi lain": writeInfo(t, pingServer(t, "aplikasi-lain")),
		"berkas rusak":               corrupt,
		"berkas tidak ada":           filepath.Join(t.TempDir(), "runtime.json"),
	}
	for name, path := range tests {
		t.Run(name, func(t *testing.T) {
			if _, ok := instance.FindRunning(path); ok {
				t.Error("dianggap instance hidup")
			}
		})
	}
}

func TestWriteDanRemove(t *testing.T) {
	path := writeInfo(t, 8080)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("baca: %v", err)
	}
	var got instance.Info
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("isi bukan JSON valid: %v", err)
	}
	if got.Port != 8080 || got.Token != "rahasia" {
		t.Errorf("isi = %+v", got)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("berkas sementara tertinggal")
	}

	if err := instance.Remove(path); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	// Shutdown bisa memanggil Remove setelah berkas hilang; itu bukan error.
	if err := instance.Remove(path); err != nil {
		t.Errorf("Remove() kedua error = %v", err)
	}
}

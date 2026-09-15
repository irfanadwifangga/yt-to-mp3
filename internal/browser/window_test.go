package browser

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAppWindowArgs(t *testing.T) {
	args := appWindowArgs("http://127.0.0.1:1234/?token=abc", `C:\Data\yt-to-mp3\window`)

	if !slices.Contains(args, "--app=http://127.0.0.1:1234/?token=abc") {
		t.Errorf("URL tidak dibuka sebagai jendela aplikasi: %v", args)
	}
	// Profil khusus wajib ada: tanpa itu penutupan jendela tidak terdeteksi.
	if !slices.ContainsFunc(args, func(a string) bool {
		return strings.HasPrefix(a, "--user-data-dir=") && strings.HasSuffix(a, `\window`)
	}) {
		t.Errorf("profil jendela tidak dipakai: %v", args)
	}
}

// TestHelperProcess bukan test sungguhan: ia berperan sebagai proses browser
// palsu yang hidup selama durasi tertentu.
func TestHelperProcess(t *testing.T) {
	d := os.Getenv("BROWSER_HELPER_SLEEP")
	if d == "" {
		return
	}
	dur, _ := time.ParseDuration(d)
	time.Sleep(dur)
	os.Exit(0)
}

func startHelper(t *testing.T, sleep string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "BROWSER_HELPER_SLEEP="+sleep)
	if err := cmd.Start(); err != nil {
		t.Fatalf("jalankan proses pembantu: %v", err)
	}
	return cmd
}

func TestWindowClosedSetelahJendelaDitutup(t *testing.T) {
	old := handoffThreshold
	handoffThreshold = 50 * time.Millisecond
	w := watch(startHelper(t, "300ms"))
	handoffThreshold = old

	select {
	case <-w.Closed():
	case <-time.After(10 * time.Second):
		t.Fatal("jendela yang ditutup tidak terdeteksi")
	}
}

// Proses yang langsung keluar karena meneruskan ke Edge lain tidak boleh
// dianggap jendela ditutup; kalau tidak, aplikasi berhenti sesaat setelah
// dibuka.
func TestWindowHandoffTidakDianggapDitutup(t *testing.T) {
	old := handoffThreshold
	handoffThreshold = 5 * time.Second
	w := watch(startHelper(t, "0s"))
	handoffThreshold = old

	select {
	case <-w.Closed():
		t.Error("peluncuran yang diteruskan dianggap jendela ditutup")
	case <-time.After(1500 * time.Millisecond):
	}
}

package browser

import (
	"errors"
	"os/exec"
	"time"
)

// ErrAppWindowUnavailable menandakan tidak ada browser yang bisa membuka
// jendela aplikasi di sistem ini. Pemanggil jatuh ke Open.
var ErrAppWindowUnavailable = errors.New("jendela aplikasi tidak tersedia")

// handoffThreshold membedakan jendela yang ditutup dari proses yang langsung
// meneruskan permintaan ke proses Edge lain untuk profil yang sama, misalnya
// ketika jendela lama masih terbuka. Yang kedua keluar dalam hitungan
// milidetik dan bukan tanda jendela ditutup.
var handoffThreshold = 5 * time.Second

// Window adalah jendela aplikasi yang sedang terbuka.
type Window struct {
	closed chan struct{}
}

// Closed ditutup ketika pengguna menutup jendela. Tidak pernah ditutup bila
// peluncuran diteruskan ke proses lain, karena penutupan jendela itu tidak
// bisa diamati dari sini.
func (w *Window) Closed() <-chan struct{} { return w.closed }

// watch mengamati proses browser sampai berakhir.
func watch(cmd *exec.Cmd) *Window {
	w := &Window{closed: make(chan struct{})}
	limit := handoffThreshold
	started := time.Now()
	go func() {
		_ = cmd.Wait()
		if time.Since(started) >= limit {
			close(w.closed)
		}
	}()
	return w
}

// appWindowArgs menyusun argumen jendela aplikasi Edge.
//
// --user-data-dir memakai profil khusus aplikasi, bukan profil Edge milik
// pengguna. Tanpa itu, Edge yang sudah terbuka menerima permintaan lalu
// proses peluncurnya langsung keluar, sehingga penutupan jendela tidak bisa
// dideteksi; jendela aplikasi juga akan tercampur dengan tab dan ekstensi
// pengguna. Profil yang tetap (bukan InPrivate) mempertahankan tema dan
// bahasa UI yang disimpan di localStorage.
func appWindowArgs(url, profileDir string) []string {
	return []string{
		"--app=" + url,
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--window-size=1180,860",
	}
}

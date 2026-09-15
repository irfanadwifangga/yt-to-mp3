//go:build !windows

package browser

// OpenAppWindow belum didukung di luar Windows: macOS dan Linux tidak
// menjamin browser berbasis Chromium terpasang, jadi UI dibuka di browser
// default lewat Open.
func OpenAppWindow(string, string) (*Window, error) {
	return nil, ErrAppWindowUnavailable
}

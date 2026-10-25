//go:build windows

package browser

import (
	"os"
	"testing"
)

func TestFindAppBrowser(t *testing.T) {
	path := findAppBrowser()
	t.Logf("ditemukan browser: %s", path)
	if path != "" {
		if fi, err := os.Stat(path); err != nil || fi.IsDir() {
			t.Errorf("findAppBrowser mengembalikan path tidak valid: %s", path)
		}
	}
}

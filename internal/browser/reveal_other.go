//go:build !windows

package browser

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// Reveal membuka file manager pada lokasi sebuah berkas, dengan berkasnya
// tersorot bila sistem mendukung.
//
// Path berasal dari database, bukan dari klien: endpoint yang menerima path
// dari luar akan menjadi jalur membuka berkas sewenang-wenang.
func Reveal(path string) error {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", "-R", path)
	} else {
		// Sebagian besar file manager Linux tidak punya opsi sorot yang
		// seragam, jadi cukup buka direktorinya.
		cmd = exec.Command("xdg-open", filepath.Dir(path))
	}
	return start(cmd, "buka lokasi berkas")
}

//go:build windows

package browser

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

// Reveal membuka Explorer dengan berkasnya tersorot.
//
// Path berasal dari database, bukan dari klien: endpoint yang menerima path
// dari luar akan menjadi jalur membuka berkas sewenang-wenang.
//
// Baris perintah disusun sendiri alih-alih lewat argumen exec.Command. Go
// membungkus argumen yang memuat spasi dengan tanda kutip utuh, menjadi
// "/select,C:\Users\Nama Lengkap\Music\lagu.mp3", dan Explorer tidak
// mengenali bentuk itu: ia diam-diam membuka folder Documents. Explorer
// hanya menerima tanda kutip di sekitar path-nya saja. Nama berkas Windows
// tidak mungkin memuat tanda kutip, jadi membungkusnya di sini aman.
func Reveal(path string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: explorerSelectCommandLine(path)}
	return start(cmd, "buka lokasi berkas")
}

// explorerSelectCommandLine menyusun baris perintah Explorer yang menyorot
// sebuah berkas. filepath.Clean juga mengubah garis miring biasa menjadi
// backslash, yang wajib bagi Explorer.
func explorerSelectCommandLine(path string) string {
	return `explorer.exe /select,"` + filepath.Clean(path) + `"`
}

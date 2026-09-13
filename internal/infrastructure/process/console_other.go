//go:build !windows

package process

import "os/exec"

// HasConsole selalu benar di luar Windows: tidak ada subsystem GUI yang
// menyembunyikan stderr, dan proses anak tidak pernah membuka jendela.
func HasConsole() bool { return true }

func hideConsole(*exec.Cmd) {}

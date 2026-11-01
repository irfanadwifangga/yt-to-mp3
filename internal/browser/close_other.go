//go:build !windows

package browser

import (
	"errors"
	"os"
)

func closeAppProcess(process *os.Process) error {
	if process == nil {
		return nil
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

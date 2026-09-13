//go:build !windows

package process

import (
	"errors"
	"syscall"
	"testing"
)

func TestIsIgnorableKillError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "ESRCH diabaikan",
			err:  syscall.ESRCH,
			want: true,
		},
		{
			name: "EPERM diabaikan",
			err:  syscall.EPERM,
			want: true,
		},
		{
			name: "error lain tidak diabaikan",
			err:  errors.New("lainnya"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isIgnorableKillError(tc.err); got != tc.want {
				t.Fatalf("isIgnorableKillError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

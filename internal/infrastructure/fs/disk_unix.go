//go:build !windows

package fs

import (
	"fmt"
	"syscall"
)

// FreeSpace mengembalikan ruang kosong yang tersedia bagi pengguna saat ini
// pada volume yang memuat path.
//
// Bavail dipakai, bukan Bfree: sebagian blok dicadangkan untuk root, dan
// proses biasa tidak akan pernah bisa memakainya.
func FreeSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("baca ruang kosong: %w", err)
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

//go:build windows

package fs

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// FreeSpace mengembalikan ruang kosong yang tersedia bagi pengguna saat ini
// pada volume yang memuat path.
//
// Yang dipakai adalah kuota pengguna, bukan ruang kosong total volume:
// pada sistem berkuota keduanya berbeda, dan yang membatasi kita adalah
// yang pertama.
func FreeSpace(path string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, fmt.Errorf("konversi path: %w", err)
	}

	var freeForUser, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeForUser, &total, &totalFree); err != nil {
		return 0, fmt.Errorf("baca ruang kosong: %w", err)
	}
	return freeForUser, nil
}

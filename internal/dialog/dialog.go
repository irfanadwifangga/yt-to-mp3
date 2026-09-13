// Package dialog membuka dialog native sistem operasi.
package dialog

import (
	"context"
	"errors"
	"fmt"

	"github.com/ncruces/zenity"
)

// PickFolder membuka dialog pemilih folder native dan mengembalikan path
// yang dipilih. ok bernilai false bila pengguna menutup dialog tanpa
// memilih.
//
// Dialog dibuka oleh proses server di mesin pengguna, bukan oleh browser.
// Browser sengaja tidak pernah memberi tahu halaman web path absolut sebuah
// folder, sedangkan aplikasi ini butuh path itu untuk menulis hasil.
//
// zenity tidak memakai cgo di Windows dan macOS; di Linux ia memanggil
// zenity, kdialog, atau qarma bila tersedia. Tanpa ketiganya error
// dikembalikan, dan UI jatuh ke input manual.
func PickFolder(ctx context.Context, title, start string) (string, bool, error) {
	opts := []zenity.Option{zenity.Directory(), zenity.Context(ctx)}
	if title != "" {
		opts = append(opts, zenity.Title(title))
	}
	if start != "" {
		opts = append(opts, zenity.Filename(start))
	}

	path, err := zenity.SelectFile(opts...)
	switch {
	case err == nil:
		return path, true, nil
	case errors.Is(err, zenity.ErrCanceled), errors.Is(err, context.Canceled):
		// Menutup dialog, atau tab yang meminta sudah ditutup, bukan kegagalan.
		return "", false, nil
	default:
		return "", false, fmt.Errorf("buka pemilih folder: %w", err)
	}
}

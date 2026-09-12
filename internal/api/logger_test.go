package api_test

import (
	"io"
	"log/slog"
)

// newDiscardLogger membuat logger yang membuang seluruh output agar keluaran
// test tetap bersih.
func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

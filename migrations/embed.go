// Package migrations menyematkan berkas migrasi SQL ke dalam binary.
//
// Disematkan supaya aplikasi tidak butuh berkas eksternal saat runtime; ini
// bagian dari target distribusi binary tunggal.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

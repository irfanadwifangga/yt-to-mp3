// Package web menyematkan hasil build SPA ke dalam binary.
//
// Direktori dist/ selalu ada di repo lewat .gitkeep sehingga go:embed tidak
// gagal compile pada clone yang masih bersih. Isinya sendiri tidak di-commit.
// Lihat docs planning "Build, dev workflow, dan rilis".
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// Dist mengembalikan isi web/dist sebagai fs.FS siap pakai.
func Dist() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}

// Built melaporkan apakah SPA sudah pernah di-build.
func Built() bool {
	f, err := embedded.Open("dist/index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

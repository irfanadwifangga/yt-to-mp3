// Package version menyimpan identitas build yang diisi lewat ldflags.
package version

// Nilai berikut di-override saat rilis:
//
//	go build -ldflags "-X .../internal/version.Version=1.2.3 -X .../internal/version.Commit=abc1234"
var (
	Version = "0.1.0-dev"
	Commit  = "unknown"
)

// AppName dipakai sebagai nama direktori data dan penanda pada /api/ping.
const AppName = "yt-to-mp3"

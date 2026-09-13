// Package tools menangani discovery, unduhan, dan verifikasi checksum
// yt-dlp serta FFmpeg.
//
// Binary tidak pernah ikut dalam artifact rilis: diunduh di mesin pengguna
// sehingga proyek ini tidak mendistribusikan ulang FFmpeg dan tidak memikul
// kewajiban LGPL/GPL. Lihat ADR-031.
package tools

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime"
)

//go:embed manifest.json
var manifestRaw []byte

// manifestSchema adalah versi skema manifest yang dipahami kode ini.
//
// Skema 2 memperkenalkan beberapa unduhan per build: sebagian sumber
// menerbitkan ffmpeg dan ffprobe sebagai arsip terpisah.
const manifestSchema = 2

// Archive adalah bentuk berkas unduhan.
type Archive string

const (
	ArchiveNone  Archive = "none"
	ArchiveZip   Archive = "zip"
	ArchiveTarXZ Archive = "tar.xz"
)

// Download adalah satu berkas yang diunduh dan diverifikasi.
type Download struct {
	URL     string   `json:"url"`
	SHA256  string   `json:"sha256"`
	Archive Archive  `json:"archive"`
	Extract []string `json:"extract"`
}

// Build adalah seluruh unduhan untuk satu tool pada satu platform.
type Build struct {
	Downloads []Download `json:"downloads"`
	Note      string     `json:"note,omitempty"`
}

// Installable melaporkan apakah entri ini siap dipasang.
//
// Satu unduhan saja tanpa checksum membuat seluruh build ditolak. Lebih baik
// fitur tidak jalan daripada menjalankan binary pihak ketiga tanpa
// verifikasi. Lihat ADR-033.
func (b Build) Installable() bool {
	if len(b.Downloads) == 0 {
		return false
	}
	for _, d := range b.Downloads {
		if d.URL == "" || d.SHA256 == "" || len(d.Extract) == 0 {
			return false
		}
	}
	return true
}

// Tool adalah satu perkakas beserta seluruh buildnya.
type Tool struct {
	Version string           `json:"version"`
	Source  string           `json:"source"`
	Builds  map[string]Build `json:"builds"`
}

// Manifest adalah isi manifest.json.
type Manifest struct {
	Schema int             `json:"schema"`
	Note   string          `json:"note"`
	Tools  map[string]Tool `json:"tools"`
}

// platformKey mengembalikan kunci build untuk platform saat ini.
func platformKey() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

// LoadManifest membaca manifest yang tersemat.
func LoadManifest() (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(manifestRaw, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Schema != manifestSchema {
		return Manifest{}, fmt.Errorf("skema manifest %d tidak didukung", m.Schema)
	}
	if len(m.Tools) == 0 {
		return Manifest{}, fmt.Errorf("manifest tidak memuat satu pun tool")
	}
	return m, nil
}

// BuildFor mengembalikan entri build untuk sebuah tool pada platform ini.
func (m Manifest) BuildFor(tool string) (Build, bool) {
	t, ok := m.Tools[tool]
	if !ok {
		return Build{}, false
	}
	b, ok := t.Builds[platformKey()]
	return b, ok
}

// VersionFor mengembalikan versi ter-pin sebuah tool.
func (m Manifest) VersionFor(tool string) string {
	return m.Tools[tool].Version
}

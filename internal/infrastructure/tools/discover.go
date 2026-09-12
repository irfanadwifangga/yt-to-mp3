package tools

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/process"
)

// Nama tool yang dikenali.
const (
	YTDLP   = "yt-dlp"
	FFmpeg  = "ffmpeg"
	FFprobe = "ffprobe"
)

// Source menyatakan dari mana sebuah tool ditemukan.
type Source string

const (
	SourceManaged Source = "managed" // dipasang oleh aplikasi
	SourceSidecar Source = "sidecar" // di samping executable
	SourcePATH    Source = "path"    // milik sistem pengguna
)

// probeTimeout membatasi pemanggilan --version.
const probeTimeout = 10 * time.Second

// cacheTTL membatasi umur hasil discovery.
//
// Tanpa batas ini, tool yang dipasang pengguna dari luar (winget, brew, apt)
// selagi aplikasi berjalan tidak akan pernah terlihat sampai restart, dan
// tidak ada yang menjelaskan kenapa. Probe versi menjalankan subprocess,
// jadi cache tetap dibutuhkan agar polling health tidak memanggilnya terus.
const cacheTTL = 30 * time.Second

// Status adalah hasil discovery satu tool.
type Status struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Path      string `json:"path"`
	Source    Source `json:"source,omitempty"`
	Pinned    string `json:"pinned_version,omitempty"`
}

// Manager menemukan dan memasang tool eksternal.
type Manager struct {
	dir      string // <data_dir>/tools
	tmpDir   string
	manifest Manifest
	log      *slog.Logger
	client   *http.Client

	mu    sync.RWMutex
	cache map[string]cacheEntry
	now   func() time.Time // dapat diganti test
}

// cacheEntry adalah hasil discovery beserta waktu pengambilannya.
type cacheEntry struct {
	status  Status
	probeAt time.Time
}

// New membuat Manager.
func New(toolsDir, tmpDir string, log *slog.Logger) (*Manager, error) {
	m, err := LoadManifest()
	if err != nil {
		return nil, err
	}
	return &Manager{
		dir:      toolsDir,
		tmpDir:   tmpDir,
		manifest: m,
		log:      log,
		client:   http.DefaultClient,
		cache:    make(map[string]cacheEntry),
		now:      time.Now,
	}, nil
}

// SetHTTPClient mengganti klien HTTP; dipakai test untuk mengarah ke server
// lokal alih-alih jaringan sungguhan.
func (m *Manager) SetHTTPClient(c *http.Client) { m.client = c }

// SetManifest mengganti manifest; dipakai test dengan entri buatan.
func (m *Manager) SetManifest(man Manifest) { m.manifest = man }

// exeName menambahkan ekstensi .exe di Windows.
func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// locate mencari binary mengikuti urutan discovery: tool terkelola lebih
// dulu, lalu sidecar, terakhir PATH milik sistem.
func (m *Manager) locate(name string) (string, Source, bool) {
	file := exeName(name)

	managed := filepath.Join(m.dir, file)
	if isExecutableFile(managed) {
		return managed, SourceManaged, true
	}

	if exe, err := os.Executable(); err == nil {
		sidecar := filepath.Join(filepath.Dir(exe), file)
		if isExecutableFile(sidecar) {
			return sidecar, SourceSidecar, true
		}
	}

	if p, err := exec.LookPath(name); err == nil {
		return p, SourcePATH, true
	}
	return "", "", false
}

// isExecutableFile melaporkan apakah path adalah berkas biasa yang ada.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Resolve mengembalikan path dan versi sebuah tool.
func (m *Manager) Resolve(ctx context.Context, name string) (string, string, error) {
	st := m.Status(ctx, name)
	if !st.Available {
		return "", "", domain.NewError(domain.CodeToolMissing, domain.ClassLocal,
			fmt.Sprintf("%s tidak ditemukan", name))
	}
	return st.Path, st.Version, nil
}

// Status menjalankan discovery dan probe versi untuk satu tool.
//
// Hasilnya di-cache selama cacheTTL supaya polling health tidak memanggil
// subprocess terus-menerus, tapi tetap menemukan tool yang baru dipasang
// pengguna dari luar tanpa perlu restart.
func (m *Manager) Status(ctx context.Context, name string) Status {
	now := m.now()

	m.mu.RLock()
	entry, ok := m.cache[name]
	m.mu.RUnlock()
	if ok && now.Sub(entry.probeAt) < cacheTTL {
		return entry.status
	}

	st := Status{Name: name, Pinned: m.pinnedVersion(name)}
	path, source, found := m.locate(name)
	if found {
		st.Available = true
		st.Path = path
		st.Source = source
		st.Version = m.probeVersion(ctx, name, path)
	}

	m.mu.Lock()
	m.cache[name] = cacheEntry{status: st, probeAt: now}
	m.mu.Unlock()
	return st
}

// pinnedVersion mengembalikan versi manifest untuk tool. ffprobe ikut entri
// ffmpeg karena keduanya berasal dari arsip yang sama.
func (m *Manager) pinnedVersion(name string) string {
	if name == FFprobe {
		name = FFmpeg
	}
	return m.manifest.VersionFor(name)
}

// probeVersion menjalankan tool untuk mengambil versi aktualnya, supaya
// laporan bug bisa dikaitkan ke versi yang benar-benar dipakai.
func (m *Manager) probeVersion(ctx context.Context, name, path string) string {
	arg := "-version"
	if name == YTDLP {
		arg = "--version"
	}

	res, err := process.Output(ctx, process.Spec{
		Bin:     path,
		Args:    []string{arg},
		Timeout: probeTimeout,
	})
	if err != nil {
		m.log.Warn("probe versi gagal", "tool", name, "error", err)
		return ""
	}

	line, _, _ := strings.Cut(strings.TrimSpace(string(res.Stdout)), "\n")
	line = strings.TrimSpace(line)
	if name == YTDLP {
		return line
	}
	// "ffmpeg version n7.1 Copyright (c) ..." -> "n7.1"
	if fields := strings.Fields(line); len(fields) >= 3 && fields[1] == "version" {
		return fields[2]
	}
	return line
}

// StatusAll mengembalikan status seluruh tool yang dibutuhkan, dalam bentuk
// port application sehingga layer api tidak perlu mengimpor paket ini.
func (m *Manager) StatusAll(ctx context.Context) map[string]application.ToolStatus {
	out := make(map[string]application.ToolStatus, 3)
	for _, name := range []string{YTDLP, FFmpeg, FFprobe} {
		out[name] = m.Status(ctx, name).toPort()
	}
	return out
}

// toPort mengubah status internal jadi bentuk yang dipakai lintas layer.
func (s Status) toPort() application.ToolStatus {
	return application.ToolStatus{
		Name:      s.Name,
		Available: s.Available,
		Version:   s.Version,
		Source:    string(s.Source),
		Pinned:    s.Pinned,
		Path:      s.Path,
	}
}

// Invalidate membuang cache discovery, dipanggil setelah instalasi.
func (m *Manager) Invalidate() {
	m.mu.Lock()
	clear(m.cache)
	m.mu.Unlock()
}

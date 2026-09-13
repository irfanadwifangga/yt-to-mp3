package application

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

const (
	// ToolUpdateInterval adalah jarak antar cek pembaruan otomatis.
	ToolUpdateInterval = 7 * 24 * time.Hour

	// toolUpdateTick adalah seberapa sering jatuh tempo cek diperiksa.
	// Aplikasi jarang hidup seminggu penuh, jadi yang menentukan adalah
	// waktu cek terakhir yang tersimpan, bukan timer sepanjang seminggu.
	toolUpdateTick = 6 * time.Hour
)

// Nama tool yang dikenali layanan pembaruan. Sama dengan nama di paket
// tools, didefinisikan ulang supaya application tidak mengimpor
// infrastructure.
const (
	toolYTDLP   = "yt-dlp"
	toolFFmpeg  = "ffmpeg"
	toolFFprobe = "ffprobe"
)

// ToolUpdateState adalah hasil cek pembaruan terakhir.
type ToolUpdateState struct {
	CheckedAt time.Time         `json:"checked_at"`
	Latest    map[string]string `json:"latest"`
}

// ToolUpdateStore menyimpan hasil cek pembaruan.
type ToolUpdateStore interface {
	Load() (ToolUpdateState, error)
	Save(ToolUpdateState) error
}

// ToolUpdateSource adalah tool manager yang juga tahu versi terbaru.
type ToolUpdateSource interface {
	StatusAll(ctx context.Context) map[string]ToolStatus
	Install(ctx context.Context, name string) error
	LatestVersion(ctx context.Context, name string) (string, error)
	InstallLatestYTDLP(ctx context.Context) (string, error)
}

// ToolService menggabungkan status tool dengan hasil cek pembaruan.
//
// Cek hanya membaca versi terbaru; tidak ada yang dipasang tanpa permintaan
// eksplisit pengguna. Lihat planning §16.3.
type ToolService struct {
	src     ToolUpdateSource
	store   ToolUpdateStore
	enabled func() bool
	now     func() time.Time
	log     *slog.Logger

	mu    sync.Mutex
	state ToolUpdateState

	checkMu sync.Mutex // satu cek atau pembaruan pada satu waktu
}

// NewToolService membuat layanan tool. enabled dibaca ulang setiap jatuh
// tempo, sehingga mematikan setelan berlaku tanpa restart.
func NewToolService(src ToolUpdateSource, store ToolUpdateStore, enabled func() bool, log *slog.Logger) *ToolService {
	s := &ToolService{src: src, store: store, enabled: enabled, now: time.Now, log: log}
	if state, err := store.Load(); err != nil {
		log.Warn("baca hasil cek pembaruan gagal", "error", err)
	} else {
		s.state = state
	}
	return s
}

// SetClock mengganti sumber waktu; dipakai test.
func (s *ToolService) SetClock(now func() time.Time) { s.now = now }

// StatusAll mengembalikan status tool beserta informasi pembaruannya.
func (s *ToolService) StatusAll(ctx context.Context) map[string]ToolStatus {
	statuses := s.src.StatusAll(ctx)

	s.mu.Lock()
	latest := s.state.Latest
	s.mu.Unlock()

	for name, st := range statuses {
		key := name
		if name == toolFFprobe {
			key = toolFFmpeg
		}
		st.Latest = latest[key]
		st.UpdateAvailable = st.Available && IsNewerVersion(st.Version, st.Latest)
		statuses[name] = st
	}
	return statuses
}

// Install memasang tool dari manifest ter-pin.
func (s *ToolService) Install(ctx context.Context, name string) error {
	return s.src.Install(ctx, name)
}

// CheckedAt mengembalikan waktu cek terakhir yang berhasil, nil bila belum
// pernah.
func (s *ToolService) CheckedAt() *time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.CheckedAt.IsZero() {
		return nil
	}
	t := s.state.CheckedAt
	return &t
}

// CheckUpdates menanyakan versi terbaru seluruh tool sekarang juga.
//
// Kegagalan sebagian tidak menghapus hasil sebelumnya untuk tool itu. Hanya
// bila semua pertanyaan gagal, misalnya karena offline, cek dianggap gagal
// dan waktu cek terakhir tidak diperbarui, sehingga dicoba lagi pada tick
// berikutnya.
func (s *ToolService) CheckUpdates(ctx context.Context) error {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	found := map[string]string{}
	var lastErr error
	for _, name := range []string{toolYTDLP, toolFFmpeg} {
		v, err := s.src.LatestVersion(ctx, name)
		if err != nil {
			lastErr = err
			s.log.Warn("cek versi terbaru gagal", "tool", name, "error", err)
			continue
		}
		found[name] = v
	}
	if len(found) == 0 {
		return domain.WrapError(domain.CodeToolUpdateCheck, domain.ClassTransient,
			"versi terbaru tidak dapat dibaca", lastErr)
	}

	s.mu.Lock()
	latest := make(map[string]string, len(s.state.Latest)+len(found))
	for k, v := range s.state.Latest {
		latest[k] = v
	}
	for k, v := range found {
		latest[k] = v
	}
	s.state = ToolUpdateState{CheckedAt: s.now().UTC(), Latest: latest}
	state := s.state
	s.mu.Unlock()

	if err := s.store.Save(state); err != nil {
		s.log.Warn("simpan hasil cek pembaruan gagal", "error", err)
	}
	s.log.Info("cek pembaruan tool", "terbaru", found)
	return nil
}

// Update memperbarui tool atas permintaan pengguna.
//
// Hanya yt-dlp yang dapat diperbarui dari aplikasi. FFmpeg tetap mengikuti
// manifest yang di-pin di rilis aplikasi (ADR-033), karena kerusakannya
// tidak mengikuti perubahan di sisi YouTube.
func (s *ToolService) Update(ctx context.Context, name string) error {
	if name != toolYTDLP {
		return domain.NewError(domain.CodeInternal, domain.ClassLocal,
			"hanya yt-dlp yang dapat diperbarui dari aplikasi")
	}

	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	version, err := s.src.InstallLatestYTDLP(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	latest := map[string]string{}
	for k, v := range s.state.Latest {
		latest[k] = v
	}
	latest[toolYTDLP] = version
	s.state.Latest = latest
	state := s.state
	s.mu.Unlock()

	if err := s.store.Save(state); err != nil {
		s.log.Warn("simpan hasil pembaruan gagal", "error", err)
	}
	return nil
}

// Run memeriksa jatuh tempo cek secara berkala sampai ctx selesai.
func (s *ToolService) Run(ctx context.Context) {
	s.checkIfDue(ctx)

	ticker := time.NewTicker(toolUpdateTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkIfDue(ctx)
		}
	}
}

// Due melaporkan apakah cek otomatis sudah waktunya dijalankan.
func (s *ToolService) Due() bool {
	if !s.enabled() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now().Sub(s.state.CheckedAt) >= ToolUpdateInterval
}

func (s *ToolService) checkIfDue(ctx context.Context) {
	if !s.Due() {
		return
	}
	if err := s.CheckUpdates(ctx); err != nil {
		s.log.Info("cek pembaruan otomatis ditunda", "error", err)
	}
}

// IsNewerVersion melaporkan apakah latest lebih baru daripada installed.
//
// Versi dibandingkan per bagian angka: "2026.08.19" untuk yt-dlp, dan
// "9.0.1-essentials_build-www.gyan.dev" atau "n7.1" untuk FFmpeg, yang
// hanya diambil awalan angkanya. Versi yang tidak bisa diurai, misalnya
// snapshot "N-126498-g…", tidak pernah dianggap tertinggal: lebih baik diam
// daripada menawarkan "pembaruan" ke versi yang mungkin justru lebih lama.
func IsNewerVersion(installed, latest string) bool {
	a, ok := versionParts(installed)
	if !ok {
		return false
	}
	b, ok := versionParts(latest)
	if !ok {
		return false
	}
	for i := range max(len(a), len(b)) {
		x, y := partAt(a, i), partAt(b, i)
		if x != y {
			return y > x
		}
	}
	return false
}

func versionParts(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "n")
	if end := strings.IndexFunc(v, func(r rune) bool {
		return r != '.' && (r < '0' || r > '9')
	}); end >= 0 {
		v = v[:end]
	}
	v = strings.Trim(v, ".")
	if v == "" {
		return nil, false
	}

	fields := strings.Split(v, ".")
	parts := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, false
		}
		parts = append(parts, n)
	}
	return parts, true
}

func partAt(parts []int, i int) int {
	if i < len(parts) {
		return parts[i]
	}
	return 0
}

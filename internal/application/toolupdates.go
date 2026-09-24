package application

import (
	"context"
	"fmt"
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

	// AppUpdateKey adalah kunci versi aplikasi ini di hasil cek pembaruan,
	// sama dengan version.AppName.
	AppUpdateKey = "yt-to-mp3"
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
	// CanUpdate dan InstallLatest mengatur pembaruan satu klik ke rilis
	// terbaru, di luar manifest ter-pin (ADR-033).
	CanUpdate(name string) bool
	InstallLatest(ctx context.Context, name string) (string, error)
	Progress() map[string]ToolProgress
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

	// Diisi SetApp saat wiring; kosong berarti pembaruan aplikasi tidak
	// pernah ditawarkan.
	appVersion string
	releaseURL string
}

// SetApp memasang versi aplikasi yang sedang berjalan dan halaman rilisnya.
// Dipanggil sekali saat wiring, sebelum Run.
func (s *ToolService) SetApp(version, releaseURL string) {
	s.appVersion = version
	s.releaseURL = releaseURL
}

// AppUpdate mengembalikan status pembaruan aplikasi dari cek terakhir.
//
// Aplikasi hanya memberi tahu, tidak pernah mengunduh atau memasang dirinya
// sendiri: memperbarui binary yang sedang berjalan lintas tiga OS jauh lebih
// berisiko daripada manfaatnya untuk aplikasi yang jarang dirilis.
func (s *ToolService) AppUpdate() AppUpdate {
	s.mu.Lock()
	latest := s.state.Latest[AppUpdateKey]
	s.mu.Unlock()

	release := isReleaseVersion(s.appVersion)
	// Versi yang sedang berjalan pasti sudah terbit. Hasil cek yang lebih
	// lama, tersimpan sebelum pengguna memasang versi ini, basi: tanpa ini
	// "versi terbaru" tampil lebih rendah daripada versi terpasang sampai cek
	// berikutnya berhasil. Due memicu cek ulang untuk kasus yang sama.
	if release && IsNewerVersion(latest, s.appVersion) {
		latest = s.appVersion
	}
	return AppUpdate{
		Current:         s.appVersion,
		Latest:          latest,
		UpdateAvailable: release && IsNewerVersion(s.appVersion, latest),
		ReleaseURL:      s.releaseURL,
	}
}

// isReleaseVersion melaporkan apakah versi berasal dari rilis resmi. Build
// dari source ("-dev") dan snapshot CI ("-snapshot") bukan untuk pengguna
// akhir; menawari mereka rilis resmi hanya mengganggu.
func isReleaseVersion(v string) bool {
	return v != "" && !strings.Contains(v, "-")
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
		st.Updatable = s.src.CanUpdate(name)
		statuses[name] = st
	}
	return statuses
}

// Install memasang tool dari manifest ter-pin.
//
// Bila tool dapat diperbarui dari aplikasi di platform ini, rilis terbaru
// yang dipasang, dengan verifikasi checksum yang sama dengan tombol
// perbarui. Memasang versi manifest di sana membuat pengguna baru langsung
// disuruh memperbarui tool yang baru saja dipasang, karena manifest hanya
// maju mengikuti rilis aplikasi. Bila rilis terbaru gagal dipasang, misalnya
// offline atau batas API GitHub, versi manifest yang dipakai, sehingga
// pemasangan tidak pernah lebih rapuh daripada sebelumnya.
func (s *ToolService) Install(ctx context.Context, name string) error {
	target := name
	if target == toolFFprobe {
		target = toolFFmpeg // keduanya berasal dari arsip yang sama
	}

	if s.src.CanUpdate(target) {
		s.checkMu.Lock()
		version, err := s.src.InstallLatest(ctx, target)
		s.checkMu.Unlock()
		if err == nil {
			s.recordLatest(target, version)
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		s.log.Warn("pasang rilis terbaru gagal, memakai versi manifest",
			"tool", target, "error", err)
	}
	return s.src.Install(ctx, name)
}

// Progress mengembalikan kemajuan instalasi yang sedang berjalan.
func (s *ToolService) Progress() map[string]ToolProgress {
	return s.src.Progress()
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
	for _, name := range []string{toolYTDLP, toolFFmpeg, AppUpdateKey} {
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

// Update memperbarui tool ke rilis terbarunya atas permintaan pengguna.
//
// yt-dlp dapat diperbarui di semua platform, FFmpeg hanya di Windows; di
// tempat lain FFmpeg mengikuti manifest yang di-pin di rilis aplikasi
// (ADR-033). Tool sumber yang menentukan lewat CanUpdate.
func (s *ToolService) Update(ctx context.Context, name string) error {
	if !s.src.CanUpdate(name) {
		return domain.NewError(domain.CodeInternal, domain.ClassLocal,
			fmt.Sprintf("%s tidak dapat diperbarui dari aplikasi di sistem ini", name))
	}

	s.checkMu.Lock()
	defer s.checkMu.Unlock()

	version, err := s.src.InstallLatest(ctx, name)
	if err != nil {
		return err
	}
	s.recordLatest(name, version)
	return nil
}

// recordLatest menyimpan versi yang baru dipasang sebagai versi terbaru,
// supaya tool itu tidak langsung ditandai tertinggal oleh hasil cek lama.
func (s *ToolService) recordLatest(name, version string) {
	s.mu.Lock()
	latest := map[string]string{}
	for k, v := range s.state.Latest {
		latest[k] = v
	}
	latest[name] = version
	s.state.Latest = latest
	state := s.state
	s.mu.Unlock()

	if err := s.store.Save(state); err != nil {
		s.log.Warn("simpan hasil pembaruan gagal", "error", err)
	}
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
	// Hasil cek yang tersimpan sebelum cek pembaruan aplikasi ada tidak
	// memuat versi aplikasi. Tanpa pengecualian ini, "versi terbaru" baru
	// terisi seminggu setelah pengguna memasang versi baru. Bila cek versi
	// aplikasi terus gagal, misalnya offline, cek diulang tiap tick
	// (6 jam), masih jauh di bawah batas API GitHub.
	stored := s.state.Latest[AppUpdateKey]
	if s.appVersion != "" && stored == "" {
		return true
	}
	// Pengguna baru memasang versi yang lebih baru daripada hasil cek
	// tersimpan, jadi hasil itu pasti basi. Ditemukan setelah rilis 0.2.0:
	// "versi terbaru" tertulis 0.1.1 sampai cek mingguan berikutnya.
	if isReleaseVersion(s.appVersion) && IsNewerVersion(stored, s.appVersion) {
		return true
	}
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

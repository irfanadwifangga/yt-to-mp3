package application

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// idleCheckInterval adalah jarak antar pemeriksaan idle. Ketelitian satu
// menit cukup untuk batas yang dihitung dalam menit.
const idleCheckInterval = time.Minute

// Setelah jendela aplikasi ditutup, aplikasi berhenti begitu tidak dipakai
// selama windowCloseGrace. Jeda ini membedakan jendela yang ditutup dari muat
// ulang halaman, yang polling-nya kembali dalam dua detik.
const (
	windowCloseGrace = 10 * time.Second
	windowCloseCheck = 2 * time.Second
)

// ActiveCounter menghitung job yang sedang berjalan dan yang antre.
type ActiveCounter interface {
	CountActive(ctx context.Context) (active, queued int, err error)
}

// IdleDeps mengumpulkan dependensi IdleMonitor.
type IdleDeps struct {
	// LastActivity mengembalikan waktu request terautentikasi terakhir.
	LastActivity func() time.Time
	// OpenStreams mengembalikan jumlah koneksi SSE yang sedang terbuka.
	OpenStreams func() int
	Jobs        ActiveCounter
	// Timeout dibaca ulang setiap pemeriksaan supaya perubahan setelan
	// berlaku tanpa restart. Nol atau negatif mematikan idle shutdown.
	Timeout func() time.Duration

	Now      func() time.Time
	Interval time.Duration
	Log      *slog.Logger
}

// IdleMonitor menghentikan aplikasi yang tidak lagi dipakai.
//
// Tanpa tray icon (ADR-021), menutup tab browser tidak menghentikan proses
// server. Monitor ini yang memastikan proses itu tidak hidup selamanya di
// latar belakang setelah pengguna selesai.
type IdleMonitor struct {
	d IdleDeps

	mu           sync.Mutex
	lastBusy     time.Time
	windowClosed bool

	wake chan struct{}
	done chan struct{}
	once sync.Once
}

// WindowClosed memberi tahu bahwa jendela aplikasi ditutup.
//
// Menutup jendela adalah niat berhenti yang jelas, jadi aplikasi berhenti
// begitu tidak ada job dan tidak ada UI yang aktif selama windowCloseGrace,
// tanpa menunggu batas idle yang dihitung dalam menit. Job yang masih
// berjalan tetap diselesaikan lebih dulu. Berlaku walau idle shutdown
// dimatikan di setelan: setelan itu tentang tab yang ditinggal terbuka,
// bukan jendela yang sengaja ditutup.
func (m *IdleMonitor) WindowClosed() {
	m.mu.Lock()
	m.windowClosed = true
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// NewIdleMonitor membuat monitor idle.
func NewIdleMonitor(d IdleDeps) *IdleMonitor {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Interval <= 0 {
		d.Interval = idleCheckInterval
	}
	return &IdleMonitor{
		d:        d,
		lastBusy: d.Now(),
		wake:     make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
}

// Done ditutup ketika aplikasi dinyatakan idle.
func (m *IdleMonitor) Done() <-chan struct{} { return m.done }

// Run memeriksa secara berkala sampai idle atau ctx selesai.
func (m *IdleMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.d.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
			// Menit terlalu kasar setelah jendela ditutup; pengguna menunggu
			// aplikasi benar-benar berhenti.
			ticker.Reset(windowCloseCheck)
		case <-ticker.C:
			if m.Idle(ctx) {
				if m.closed() {
					m.d.Log.Info("jendela aplikasi ditutup dan tidak ada job, berhenti")
				} else {
					m.d.Log.Info("tidak dipakai melewati batas idle, berhenti",
						"batas", m.d.Timeout())
				}
				m.once.Do(func() { close(m.done) })
				return
			}
		}
	}
}

// Idle melaporkan apakah aplikasi sudah tidak dipakai melewati batas.
//
// Aplikasi dianggap dipakai selama ada job aktif atau antre, ada stream SSE
// terbuka, atau ada request dalam rentang batas. Jam idle dihitung dari
// kejadian terbaru di antara request terakhir dan saat terakhir aplikasi
// terlihat sibuk: job yang selesai tanpa ada tab terbuka tidak boleh
// langsung memicu berhenti hanya karena request terakhirnya sudah lama.
//
// Kegagalan membaca database dianggap sibuk. Menghentikan aplikasi karena
// tidak bisa memastikan keadaan justru bisa memutus job yang sedang jalan.
func (m *IdleMonitor) Idle(ctx context.Context) bool {
	timeout := m.d.Timeout()
	if m.closed() && (timeout <= 0 || timeout > windowCloseGrace) {
		timeout = windowCloseGrace
	}
	if timeout <= 0 {
		return false
	}

	now := m.d.Now()
	if m.d.OpenStreams() > 0 || m.jobsBusy(ctx) {
		m.mu.Lock()
		m.lastBusy = now
		m.mu.Unlock()
		return false
	}

	m.mu.Lock()
	since := m.lastBusy
	m.mu.Unlock()
	if last := m.d.LastActivity(); last.After(since) {
		since = last
	}
	return now.Sub(since) >= timeout
}

func (m *IdleMonitor) closed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.windowClosed
}

func (m *IdleMonitor) jobsBusy(ctx context.Context) bool {
	active, queued, err := m.d.Jobs.CountActive(ctx)
	if err != nil {
		m.d.Log.Warn("hitung job untuk idle gagal, dianggap sibuk", "error", err)
		return true
	}
	return active+queued > 0
}

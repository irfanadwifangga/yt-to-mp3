package api

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

const (
	// maxSubscribersPerJob membatasi jumlah tab yang memantau satu job.
	maxSubscribersPerJob = 4

	// subscriberBuffer menahan lonjakan sesaat sebelum pelanggan lambat
	// mulai kehilangan event progress.
	subscriberBuffer = 64

	// progressInterval membatasi laju event progress ke 4 per detik.
	progressInterval = 250 * time.Millisecond
)

// EventHistory membaca event yang sudah dipersist, dipakai untuk resume.
type EventHistory interface {
	Events(ctx context.Context, jobID string, afterSeq int64) ([]domain.Event, error)
}

// Hub menyiarkan event job ke pelanggan SSE yang sedang terhubung.
//
// Hanya event state, done, dan error yang punya nomor urut dan dapat
// di-replay; progress hidup di memori saja (ADR-023). Konsekuensinya,
// Last-Event-ID selalu menunjuk event terpersist, dan pelanggan yang
// menyambung ulang menerima satu snapshot progress terkini alih-alih
// ribuan frame lama.
type Hub struct {
	history EventHistory
	log     *slog.Logger

	mu     sync.Mutex
	subs   map[string]map[*subscriber]struct{}
	latest map[string]application.StreamEvent // snapshot progress per job
	lastAt map[string]time.Time               // waktu progress terakhir dikirim
}

type subscriber struct {
	live chan application.StreamEvent
	out  chan application.StreamEvent
	done chan struct{}
	once sync.Once
}

// NewHub membuat hub SSE.
func NewHub(history EventHistory, log *slog.Logger) *Hub {
	return &Hub{
		history: history,
		log:     log,
		subs:    make(map[string]map[*subscriber]struct{}),
		latest:  make(map[string]application.StreamEvent),
		lastAt:  make(map[string]time.Time),
	}
}

// Publish menyiarkan satu event ke seluruh pelanggan job tersebut.
//
// Event progress bersifat lossy: bila pelanggan tertinggal, frame lama
// dibuang karena hanya nilai terbaru yang berguna. Event terpersist tidak
// pernah dibuang diam-diam; kegagalannya dicatat sebagai bug.
func (h *Hub) Publish(ev application.StreamEvent) {
	h.mu.Lock()

	if ev.Type == application.StreamProgress {
		h.latest[ev.JobID] = ev

		// Throttle: UI tidak mendapat manfaat dari lebih dari empat
		// pembaruan per detik, sementara biayanya nyata di seluruh rantai.
		if last, ok := h.lastAt[ev.JobID]; ok && time.Since(last) < progressInterval {
			h.mu.Unlock()
			return
		}
		h.lastAt[ev.JobID] = time.Now()
	}

	targets := make([]*subscriber, 0, len(h.subs[ev.JobID]))
	for sub := range h.subs[ev.JobID] {
		targets = append(targets, sub)
	}
	h.mu.Unlock()

	for _, sub := range targets {
		select {
		case sub.live <- ev:
		default:
			// Buffer penuh. Progress boleh hilang; selain itu berarti
			// pelanggan macet dan koneksinya lebih baik diputus daripada
			// menahan publisher.
			if ev.Type != application.StreamProgress {
				h.log.Warn("pelanggan SSE tertinggal, koneksi ditutup",
					"job", ev.JobID, "type", ev.Type)
				sub.close()
			}
		}
	}
}

// Forget membuang state siaran sebuah job setelah terminal.
func (h *Hub) Forget(jobID string) {
	h.mu.Lock()
	delete(h.latest, jobID)
	delete(h.lastAt, jobID)
	h.mu.Unlock()
}

// Count mengembalikan jumlah pelanggan aktif sebuah job.
func (h *Hub) Count(jobID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[jobID])
}

// Streams mengembalikan jumlah seluruh koneksi SSE yang sedang terbuka.
func (h *Hub) Streams() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	var n int
	for _, set := range h.subs {
		n += len(set)
	}
	return n
}

// Subscribe mendaftarkan pelanggan baru dan mengalirkan event yang terlewat.
//
// Urutan operasinya penting dan sengaja tidak intuitif: listener live
// dipasang lebih dulu, baru histori dibaca. Versi naif yang membaca histori
// dulu akan kehilangan setiap event yang terjadi di celah antara pembacaan
// dan pemasangan listener.
//
// closeAfterHistory dipakai untuk job yang sudah terminal: pelanggan
// menerima histori beserta state akhirnya, lalu stream ditutup alih-alih
// menggantung menunggu event yang tidak akan pernah datang.
func (h *Hub) Subscribe(
	ctx context.Context, jobID string, lastEventID int64, closeAfterHistory bool,
) (<-chan application.StreamEvent, func(), error) {
	sub := &subscriber{
		live: make(chan application.StreamEvent, subscriberBuffer),
		out:  make(chan application.StreamEvent, subscriberBuffer),
		done: make(chan struct{}),
	}

	h.mu.Lock()
	if len(h.subs[jobID]) >= maxSubscribersPerJob {
		h.mu.Unlock()
		return nil, nil, domain.NewError(domain.CodeRateLimited, domain.ClassLocal,
			"terlalu banyak koneksi untuk job ini")
	}
	if h.subs[jobID] == nil {
		h.subs[jobID] = make(map[*subscriber]struct{})
	}
	h.subs[jobID][sub] = struct{}{}
	snapshot, hasSnapshot := h.latest[jobID]
	h.mu.Unlock()

	unsubscribe := func() {
		h.mu.Lock()
		if set, ok := h.subs[jobID]; ok {
			delete(set, sub)
			if len(set) == 0 {
				delete(h.subs, jobID)
			}
		}
		h.mu.Unlock()
		sub.close()
	}

	history, err := h.history.Events(ctx, jobID, lastEventID)
	if err != nil {
		unsubscribe()
		return nil, nil, err
	}

	go h.pump(sub, history, snapshot, hasSnapshot, closeAfterHistory)
	return sub.out, unsubscribe, nil
}

// pump mengirim histori lebih dulu, lalu meneruskan aliran live sambil
// membuang event yang sudah termuat di histori.
func (h *Hub) pump(
	sub *subscriber,
	history []domain.Event,
	snapshot application.StreamEvent,
	hasSnapshot bool,
	closeAfterHistory bool,
) {
	defer close(sub.out)

	var highest int64
	for _, ev := range history {
		if ev.Seq > highest {
			highest = ev.Seq
		}
		// Payload dibaca kembali sebagai struktur penuh, supaya event hasil
		// replay identik bentuknya dengan event live.
		replayed := application.ParseEventPayload(ev.Payload)
		replayed.JobID = ev.JobID
		replayed.Seq = ev.Seq
		replayed.Type = application.StreamEventType(ev.Type)

		if !sub.send(replayed) {
			return
		}
	}

	// Satu snapshot progress menggantikan replay ribuan frame.
	if hasSnapshot && !sub.send(snapshot) {
		return
	}

	if closeAfterHistory {
		return // job sudah terminal; tidak akan ada event lagi
	}

	for {
		select {
		case <-sub.done:
			return
		case ev := <-sub.live:
			// Event terpersist yang sudah ikut terkirim lewat histori
			// dibuang supaya pelanggan tidak menerimanya dua kali.
			if ev.Seq > 0 && ev.Seq <= highest {
				continue
			}
			if !sub.send(ev) {
				return
			}
		}
	}
}

// send mengirim ke pelanggan, atau berhenti bila koneksinya sudah ditutup.
func (s *subscriber) send(ev application.StreamEvent) bool {
	select {
	case s.out <- ev:
		return true
	case <-s.done:
		return false
	}
}

func (s *subscriber) close() {
	s.once.Do(func() { close(s.done) })
}

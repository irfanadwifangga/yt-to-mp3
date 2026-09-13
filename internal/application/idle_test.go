package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
)

type fakeCounter struct {
	active, queued int
	err            error
}

func (f *fakeCounter) CountActive(context.Context) (int, int, error) {
	return f.active, f.queued, f.err
}

type idleWorld struct {
	now      time.Time
	activity time.Time
	streams  int
	jobs     *fakeCounter
	timeout  time.Duration
}

func newIdleWorld() (*idleWorld, *application.IdleMonitor) {
	start := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	w := &idleWorld{now: start, activity: start, jobs: &fakeCounter{}, timeout: 30 * time.Minute}
	m := application.NewIdleMonitor(application.IdleDeps{
		LastActivity: func() time.Time { return w.activity },
		OpenStreams:  func() int { return w.streams },
		Jobs:         w.jobs,
		Timeout:      func() time.Duration { return w.timeout },
		Now:          func() time.Time { return w.now },
		Log:          discard(),
	})
	return w, m
}

func TestIdleSetelahBatasTanpaAktivitas(t *testing.T) {
	w, m := newIdleWorld()
	ctx := context.Background()

	w.now = w.now.Add(29 * time.Minute)
	if m.Idle(ctx) {
		t.Fatal("idle sebelum batas tercapai")
	}
	w.now = w.now.Add(time.Minute)
	if !m.Idle(ctx) {
		t.Fatal("tidak idle walau batas tercapai")
	}
}

// Tab yang terbuka melakukan polling, jadi request baru menunda idle.
func TestIdleDitundaRequest(t *testing.T) {
	w, m := newIdleWorld()

	w.now = w.now.Add(25 * time.Minute)
	w.activity = w.now
	w.now = w.now.Add(25 * time.Minute)

	if m.Idle(context.Background()) {
		t.Error("idle padahal request terakhir baru 25 menit lalu")
	}
}

// Job yang selesai tanpa tab terbuka tidak boleh langsung memicu berhenti
// hanya karena request terakhirnya sudah lama: jam idle mulai dari saat
// aplikasi terakhir terlihat sibuk.
func TestIdleDihitungDariSaatTerakhirSibuk(t *testing.T) {
	w, m := newIdleWorld()
	ctx := context.Background()

	w.jobs.active = 1
	w.now = w.now.Add(40 * time.Minute)
	if m.Idle(ctx) {
		t.Fatal("idle padahal job masih berjalan")
	}

	w.jobs.active = 0 // job selesai
	w.now = w.now.Add(time.Minute)
	if m.Idle(ctx) {
		t.Fatal("langsung idle begitu job selesai")
	}

	w.now = w.now.Add(30 * time.Minute)
	if !m.Idle(ctx) {
		t.Fatal("tidak idle 30 menit setelah job terakhir selesai")
	}
}

func TestIdleTidakTerjadiSelamaSibuk(t *testing.T) {
	tests := map[string]func(w *idleWorld){
		"stream terbuka":  func(w *idleWorld) { w.streams = 1 },
		"job antre":       func(w *idleWorld) { w.jobs.queued = 1 },
		"database gagal":  func(w *idleWorld) { w.jobs.err = errors.New("terkunci") },
		"batas dimatikan": func(w *idleWorld) { w.timeout = 0 },
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			w, m := newIdleWorld()
			setup(w)
			w.now = w.now.Add(10 * time.Hour)
			if m.Idle(context.Background()) {
				t.Error("aplikasi dinyatakan idle")
			}
		})
	}
}

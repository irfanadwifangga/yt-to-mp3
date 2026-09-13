package tools

import (
	"sync"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
)

// progressState menyimpan kemajuan instalasi yang sedang berjalan.
//
// Instalasi berjalan di dalam satu request HTTP yang panjang, jadi
// kemajuannya tidak bisa ikut di respons request itu. UI membacanya lewat
// GET /api/tools selagi request pemasangan masih tertahan.
type progressState struct {
	mu    sync.Mutex
	items map[string]application.ToolProgress
}

// Progress mengembalikan salinan kemajuan instalasi yang sedang berjalan,
// dikunci per nama tool. Tidak pernah nil.
func (m *Manager) Progress() map[string]application.ToolProgress {
	m.progress.mu.Lock()
	defer m.progress.mu.Unlock()

	out := make(map[string]application.ToolProgress, len(m.progress.items))
	for name, p := range m.progress.items {
		out[name] = p
	}
	return out
}

func (m *Manager) setProgress(name string, p application.ToolProgress) {
	m.progress.mu.Lock()
	defer m.progress.mu.Unlock()
	if m.progress.items == nil {
		m.progress.items = make(map[string]application.ToolProgress)
	}
	m.progress.items[name] = p
}

func (m *Manager) clearProgress(name string) {
	m.progress.mu.Lock()
	defer m.progress.mu.Unlock()
	delete(m.progress.items, name)
}

// byteCounter melaporkan jumlah byte yang sudah tertulis.
type byteCounter struct {
	done, total int64
	report      func(done, total int64)
}

func (c *byteCounter) Write(p []byte) (int, error) {
	c.done += int64(len(p))
	if c.report != nil {
		c.report(c.done, c.total)
	}
	return len(p), nil
}

// Package preview owns bounded, temporary preview files independently of download history.
package preview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

const MaxFileBytes int64 = 80 << 20
const maxWorkBytes int64 = 192 << 20
const maxTotalBytes int64 = 256 << 20
const ttl = 15 * time.Minute
const timeout = 5 * time.Minute

type Builder interface {
	Build(context.Context, string, string, func(string, *float64)) error
}
type Status struct {
	ID        string           `json:"id"`
	State     string           `json:"state"`
	Phase     string           `json:"phase"`
	Percent   *float64         `json:"percent,omitempty"`
	ErrorCode domain.ErrorCode `json:"error_code,omitempty"`
}
type item struct {
	status  Status
	dir     string
	used    time.Time
	cancel  context.CancelFunc
	running bool
}
type Manager struct {
	mu      sync.Mutex
	root    string
	builder Builder
	ctx     context.Context
	cancel  context.CancelFunc
	items   map[string]*item
	active  *item
	closed  bool
	wg      sync.WaitGroup
}

func New(parent context.Context, root string, builder Builder) (*Manager, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	m := &Manager{root: root, builder: builder, ctx: ctx, cancel: cancel, items: map[string]*item{}}
	m.sweep(time.Now())
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				m.sweep(now)
			}
		}
	}()
	return m, nil
}
func snapshot(j *item) Status {
	s := j.status
	if s.Percent != nil {
		p := *s.Percent
		s.Percent = &p
	}
	return s
}
func missing() error {
	return domain.NewError(domain.CodePreviewNotFound, domain.ClassPermanent, "preview tidak tersedia")
}
func (m *Manager) Create(raw string) (Status, error) {
	key, invalid := domain.NormalizeURL(raw)
	if invalid != nil {
		return Status{}, invalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Status{}, missing()
	}
	if m.active != nil {
		return Status{}, domain.NewError(domain.CodePreviewBusy, domain.ClassLocal, "preview lain sedang disiapkan")
	}
	if len(m.items) >= 2 {
		var oldest *item
		for _, j := range m.items {
			if oldest == nil || j.used.Before(oldest.used) {
				oldest = j
			}
		}
		delete(m.items, oldest.status.ID)
		_ = os.RemoveAll(oldest.dir)
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return Status{}, err
	}
	id := hex.EncodeToString(bytes[:])
	dir := filepath.Join(m.root, "preview-"+id)
	if err := os.Mkdir(dir, 0700); err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(m.ctx, timeout)
	j := &item{status: Status{ID: id, State: "preparing", Phase: "downloading"}, dir: dir, used: time.Now(), cancel: cancel, running: true}
	m.items[id] = j
	m.active = j
	m.wg.Add(1)
	go m.build(ctx, j, domainURL(key))
	return snapshot(j), nil
}
func domainURL(key string) string { return "https://www.youtube.com/watch?v=" + domain.VideoID(key) }
func (m *Manager) build(ctx context.Context, j *item, url string) {
	defer m.wg.Done()
	defer j.cancel()
	stop := make(chan struct{})
	watched := make(chan struct{})
	limited := false
	go func() {
		defer close(watched)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if diskSize(j.dir) > maxWorkBytes || diskSize(m.root) > maxTotalBytes {
					limited = true
					j.cancel()
					return
				}
			}
		}
	}()
	err := m.builder.Build(ctx, url, j.dir, func(phase string, p *float64) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if !j.running {
			return
		}
		j.status.Phase = phase
		j.status.Percent = nil
		if p != nil && !math.IsNaN(*p) && !math.IsInf(*p, 0) {
			value := math.Max(0, math.Min(100, *p))
			j.status.Percent = &value
		}
	})
	close(stop)
	<-watched
	if err == nil {
		info, statErr := os.Lstat(filepath.Join(j.dir, "preview.mp4"))
		if statErr != nil {
			err = statErr
		} else if !info.Mode().IsRegular() || info.Size() == 0 {
			err = errors.New("invalid preview output")
		} else if info.Size() > MaxFileBytes {
			limited = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	j.running = false
	m.active = nil
	if limited {
		err = domain.NewError(domain.CodePreviewTooLarge, domain.ClassLocal, "preview melewati batas penyimpanan")
	}
	if err != nil || ctx.Err() != nil {
		j.status.State = "failed"
		j.status.Percent = nil
		j.status.ErrorCode = domain.CodeDownloadFailed
		var de *domain.Error
		if errors.As(err, &de) {
			j.status.ErrorCode = de.Code
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			j.status.ErrorCode = domain.CodeTimeout
		} else if ctx.Err() != nil {
			j.status.ErrorCode = domain.CodeCancelled
		}
		_ = os.RemoveAll(j.dir)
	} else {
		j.status.State = "ready"
		j.status.Phase = "ready"
		p := 100.0
		j.status.Percent = &p
	}
	if m.items[j.status.ID] != j {
		_ = os.RemoveAll(j.dir)
	}
	j.used = time.Now()
}
func (m *Manager) Get(id string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.items[id]
	if j == nil {
		return Status{}, missing()
	}
	j.used = time.Now()
	return snapshot(j), nil
}
func (m *Manager) Open(id string) (*os.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j := m.items[id]
	if j == nil || j.status.State != "ready" {
		return nil, missing()
	}
	j.used = time.Now()
	return os.Open(filepath.Join(j.dir, "preview.mp4"))
}
func (m *Manager) Delete(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j := m.items[id]; j != nil {
		j.cancel()
		delete(m.items, id)
		if !j.running {
			_ = os.RemoveAll(j.dir)
		}
	}
}
func owned(name string) bool {
	if !strings.HasPrefix(name, "preview-") {
		return false
	}
	id := strings.TrimPrefix(name, "preview-")
	_, err := hex.DecodeString(id)
	return len(id) == 32 && err == nil
}
func (m *Manager) sweep(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, j := range m.items {
		if !j.running && now.Sub(j.used) > ttl {
			delete(m.items, id)
			_ = os.RemoveAll(j.dir)
		}
	}
	entries, _ := os.ReadDir(m.root)
	for _, entry := range entries {
		if !owned(entry.Name()) {
			continue
		}
		id := strings.TrimPrefix(entry.Name(), "preview-")
		if m.items[id] == nil && (m.active == nil || m.active.status.ID != id) {
			_ = os.RemoveAll(filepath.Join(m.root, entry.Name()))
		}
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	for _, j := range m.items {
		j.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	m.mu.Lock()
	for id, j := range m.items {
		_ = os.RemoveAll(j.dir)
		delete(m.items, id)
	}
	m.mu.Unlock()
	m.sweep(time.Now())
}
func diskSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

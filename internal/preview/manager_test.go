package preview

import (
	"context"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type buildFunc func(context.Context, string, string, func(string, *float64)) error

func (f buildFunc) Build(c context.Context, u, d string, p func(string, *float64)) error {
	return f(c, u, d, p)
}
func readyBuilder(_ context.Context, _ string, dir string, p func(string, *float64)) error {
	v := 50.0
	p("encoding", &v)
	return os.WriteFile(filepath.Join(dir, "preview.mp4"), []byte("0123456789"), 0600)
}
func waitState(t *testing.T, m *Manager, id, state string) Status {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		s, e := m.Get(id)
		if e == nil && s.State == state {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("preview did not reach %s", state)
	return Status{}
}
func TestReadyCacheExpiryAndOwnedCleanup(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "keep.txt"), []byte("keep"), 0600)
	m, e := New(context.Background(), root, buildFunc(readyBuilder))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	if _, e = m.Create("file:///not-allowed"); e == nil {
		t.Fatal("accepted invalid source")
	}
	first, e := m.Create("https://youtu.be/aaaaaaaaaaa")
	if e != nil {
		t.Fatal(e)
	}
	waitState(t, m, first.ID, "ready")
	f, e := m.Open(first.ID)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	second, e := m.Create("https://youtu.be/bbbbbbbbbbb")
	if e != nil {
		t.Fatal(e)
	}
	waitState(t, m, second.ID, "ready")
	third, e := m.Create("https://youtu.be/ccccccccccc")
	if e != nil {
		t.Fatal(e)
	}
	waitState(t, m, third.ID, "ready")
	if _, e = m.Get(first.ID); e == nil {
		t.Fatal("oldest preview was not evicted")
	}
	m.sweep(time.Now().Add(ttl + time.Second))
	if _, e = m.Open(third.ID); e == nil {
		t.Fatal("expired preview is still readable")
	}
	if _, e = os.Stat(filepath.Join(root, "keep.txt")); e != nil {
		t.Fatal("cleanup removed unrelated file")
	}
}
func TestCancellationAndSingleActiveBuild(t *testing.T) {
	done := make(chan struct{})
	root := t.TempDir()
	m, e := New(context.Background(), root, buildFunc(func(ctx context.Context, _ string, dir string, _ func(string, *float64)) error {
		_ = os.WriteFile(filepath.Join(dir, "partial"), []byte("partial"), 0600)
		<-ctx.Done()
		close(done)
		return ctx.Err()
	}))
	if e != nil {
		t.Fatal(e)
	}
	job, e := m.Create("https://youtu.be/aaaaaaaaaaa")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Create("https://youtu.be/bbbbbbbbbbb"); e == nil {
		t.Fatal("concurrent preview accepted")
	}
	m.Delete(job.ID)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("builder was not cancelled")
	}
	m.Close()
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("cancelled preview left files")
	}
}
func TestOversizedPreviewIsRejectedAndDeleted(t *testing.T) {
	root := t.TempDir()
	m, e := New(context.Background(), root, buildFunc(func(_ context.Context, _ string, d string, _ func(string, *float64)) error {
		f, e := os.Create(filepath.Join(d, "preview.mp4"))
		if e != nil {
			return e
		}
		defer f.Close()
		return f.Truncate(MaxFileBytes + 1)
	}))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	job, e := m.Create("https://youtu.be/aaaaaaaaaaa")
	if e != nil {
		t.Fatal(e)
	}
	s := waitState(t, m, job.ID, "failed")
	if s.ErrorCode != domain.CodePreviewTooLarge {
		t.Fatalf("unexpected error %s", s.ErrorCode)
	}
	if _, e = os.Stat(filepath.Join(root, "preview-"+job.ID)); !os.IsNotExist(e) {
		t.Fatal("oversized output still exists")
	}
}

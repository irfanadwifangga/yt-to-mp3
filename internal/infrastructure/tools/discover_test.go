package tools

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newDiscoveryManager membuat Manager dengan jam yang dapat dikendalikan.
func newDiscoveryManager(t *testing.T) (*Manager, *time.Time) {
	t.Helper()
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := &Manager{
		dir:      filepath.Join(t.TempDir(), "tools"),
		log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		cache:    make(map[string]cacheEntry),
		now:      func() time.Time { return clock },
		manifest: Manifest{Schema: 1, Tools: map[string]Tool{}},
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		t.Fatalf("siapkan dir: %v", err)
	}
	return m, &clock
}

// Tool yang dipasang pengguna dari luar (winget, brew, apt) selagi aplikasi
// berjalan harus terlihat tanpa restart. Sebelum ada TTL, cache menahan
// status "belum tersedia" selamanya.
func TestStatusMenemukanToolYangBaruDipasang(t *testing.T) {
	m, clock := newDiscoveryManager(t)
	const name = "alat-uji"
	ctx := context.Background()

	if st := m.Status(ctx, name); st.Available {
		t.Fatal("tool seharusnya belum tersedia di awal")
	}

	// Pemasangan dari luar, tanpa lewat Install().
	binary := filepath.Join(m.dir, exeName(name))
	if err := os.WriteFile(binary, []byte("bukan binary sungguhan"), 0o755); err != nil {
		t.Fatalf("tulis binary: %v", err)
	}

	if st := m.Status(ctx, name); st.Available {
		t.Error("dalam TTL, hasil lama seharusnya masih dipakai")
	}

	*clock = clock.Add(cacheTTL + time.Second)

	st := m.Status(ctx, name)
	if !st.Available {
		t.Error("setelah TTL lewat, tool seharusnya terdeteksi")
	}
	if st.Source != SourceManaged {
		t.Errorf("source = %q, mau %q", st.Source, SourceManaged)
	}
}

// Cache tetap ada gunanya: probe versi menjalankan subprocess, jadi polling
// health tidak boleh memanggilnya pada setiap request.
func TestStatusMemakaiCacheDalamTTL(t *testing.T) {
	m, clock := newDiscoveryManager(t)
	const name = "alat-uji"
	ctx := context.Background()

	binary := filepath.Join(m.dir, exeName(name))
	if err := os.WriteFile(binary, []byte("bukan binary sungguhan"), 0o755); err != nil {
		t.Fatalf("tulis binary: %v", err)
	}

	first := m.Status(ctx, name)
	if !first.Available {
		t.Fatal("tool seharusnya terdeteksi")
	}

	if err := os.Remove(binary); err != nil {
		t.Fatalf("hapus binary: %v", err)
	}

	*clock = clock.Add(cacheTTL / 2)
	if st := m.Status(ctx, name); !st.Available {
		t.Error("dalam TTL, hasil cache seharusnya masih dipakai")
	}

	*clock = clock.Add(cacheTTL)
	if st := m.Status(ctx, name); st.Available {
		t.Error("setelah TTL lewat, tool yang hilang seharusnya terdeteksi hilang")
	}
}

// Invalidate dipanggil setelah instalasi dan harus langsung berlaku tanpa
// menunggu TTL.
func TestInvalidateMembuangCacheSeketika(t *testing.T) {
	m, _ := newDiscoveryManager(t)
	const name = "alat-uji"
	ctx := context.Background()

	if st := m.Status(ctx, name); st.Available {
		t.Fatal("tool seharusnya belum tersedia")
	}

	binary := filepath.Join(m.dir, exeName(name))
	if err := os.WriteFile(binary, []byte("bukan binary sungguhan"), 0o755); err != nil {
		t.Fatalf("tulis binary: %v", err)
	}
	m.Invalidate()

	if st := m.Status(ctx, name); !st.Available {
		t.Error("setelah Invalidate, tool seharusnya langsung terdeteksi")
	}
}

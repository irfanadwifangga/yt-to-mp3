//go:build toolinstall

// Test ini mengunduh tool sungguhan dari manifest ter-pin, jadi hanya jalan
// dengan build tag toolinstall dan butuh jaringan. Dipakai workflow regresi
// tool setiap kali manifest berubah.
//
//	YT2MP3_TEST_TOOLS_DIR=/tmp/tools go test -tags toolinstall -run TestInstallDariManifest ./internal/infrastructure/tools/

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

func TestInstallDariManifest(t *testing.T) {
	dir := os.Getenv("YT2MP3_TEST_TOOLS_DIR")
	if dir == "" {
		dir = t.TempDir()
	}

	m, err := New(dir, filepath.Join(t.TempDir(), "tmp"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	for _, name := range []string{YTDLP, FFmpeg} {
		start := time.Now()
		if err := m.Install(ctx, name); err != nil {
			t.Fatalf("Install(%s) error = %v", name, err)
		}
		t.Logf("%s terpasang dalam %s", name, time.Since(start).Round(time.Second))
	}

	// Terpasang saja belum cukup: binary harus bisa dijalankan di platform
	// ini dan berasal dari direktori terkelola, bukan tool lain di PATH.
	for _, name := range []string{YTDLP, FFmpeg, FFprobe} {
		st := m.Status(ctx, name)
		if !st.Available || st.Source != SourceManaged {
			t.Errorf("%s: available=%v source=%q, mau managed", name, st.Available, st.Source)
			continue
		}
		if st.Version == "" {
			t.Errorf("%s: versi tidak terbaca; binary mungkin tidak cocok untuk platform ini", name)
		}
		t.Logf("%s %s (pin %s)", name, st.Version, st.Pinned)
	}
}

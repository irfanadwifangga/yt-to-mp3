package tools

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
)

// Progres unduhan harus terbaca selagi instalasi berjalan, dan hilang
// setelah selesai supaya UI tidak menampilkan bar yang macet.
func TestInstallMelaporkanProgres(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 64<<10)
	halfSent := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload[:len(payload)/2])
		w.(http.Flusher).Flush()
		close(halfSent)
		<-release
		_, _ = w.Write(payload[len(payload)/2:])
	}))
	t.Cleanup(srv.Close)
	// Cleanup berjalan terbalik: handler dilepas dulu sebelum server ditutup.
	t.Cleanup(unblock)

	m := newManager(t, nil, Build{Downloads: []Download{{
		URL: srv.URL + "/artifact", SHA256: sum(payload),
		Archive: ArchiveNone, Extract: []string{"probe-tool"},
	}}})

	done := make(chan error, 1)
	go func() { done <- m.Install(context.Background(), "probe-tool") }()

	<-halfSent
	var got application.ToolProgress
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if got = m.Progress()["probe-tool"]; got.DoneBytes > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got.Phase != application.ToolPhaseDownloading || got.Step != 1 || got.Steps != 1 ||
		got.TotalBytes != int64(len(payload)) || got.DoneBytes == 0 || got.DoneBytes >= got.TotalBytes {
		t.Errorf("progres saat mengunduh = %+v", got)
	}

	unblock()
	if err := <-done; err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if p := m.Progress(); len(p) != 0 {
		t.Errorf("progres tersisa setelah selesai: %+v", p)
	}
}

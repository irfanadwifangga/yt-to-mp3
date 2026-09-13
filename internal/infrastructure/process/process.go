// Package process menjalankan proses anak.
//
// Saat ini hanya menyediakan eksekusi sekali jalan yang menangkap output,
// cukup untuk pemanggilan metadata dan probe versi. Streaming progress dan
// terminasi seluruh process tree (ADR-012) menyusul pada tahap 5 roadmap,
// ketika pipeline unduhan membutuhkannya.
package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// DefaultMaxOutput membatasi jumlah byte yang ditahan di memori per stream.
const DefaultMaxOutput = 8 << 20

// Spec mendeskripsikan satu pemanggilan proses.
type Spec struct {
	Bin     string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration
}

// Result adalah hasil eksekusi yang sudah selesai.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// ErrTimeout dikembalikan ketika proses melewati batas waktu.
var ErrTimeout = errors.New("proses melewati batas waktu")

// Output menjalankan proses sampai selesai dan mengembalikan outputnya.
//
// Perintah selalu dijalankan sebagai argv; tidak pernah ada shell yang
// terlibat, sehingga isi argumen tidak dapat diinterpretasi sebagai perintah.
func Output(ctx context.Context, s Spec) (Result, error) {
	if s.Bin == "" {
		return Result{}, errors.New("bin kosong")
	}

	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, s.Bin, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = s.Env
	hideConsole(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &stdout, limit: DefaultMaxOutput}
	cmd.Stderr = &limitedWriter{buf: &stderr, limit: DefaultMaxOutput}

	err := cmd.Run()
	res := Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}

	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}

	switch {
	case err == nil:
		return res, nil
	case ctx.Err() != nil:
		return res, fmt.Errorf("%w setelah %s", ErrTimeout, s.Timeout)
	default:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return res, nil // exit code bukan nol dilaporkan lewat Result
		}
		return res, fmt.Errorf("jalankan %s: %w", s.Bin, err)
	}
}

// limitedWriter menahan output pada batas tertentu agar proses yang cerewet
// tidak menghabiskan memori.
type limitedWriter struct {
	buf     *bytes.Buffer
	limit   int
	written int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.written
	if remaining <= 0 {
		return len(p), nil // dibuang, tetap laporkan sukses agar proses jalan terus
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	n, err := w.buf.Write(p)
	w.written += n
	return len(p), err
}

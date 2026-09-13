// Command nfr mengukur target non-functional requirement (planning §22)
// terhadap binary rilis, lalu mencetak tabel Markdown.
//
// Setiap pengukuran berjalan pada direktori data sementara, jadi data dan
// instance milik pengguna tidak tersentuh.
//
//	go run ./scripts/nfr                        tanpa jaringan
//	go run ./scripts/nfr -url <tautan YouTube>  ikut mengukur konversi
//
// Mode -url butuh yt-dlp dan FFmpeg di PATH, dan mengunduh tautan yang
// diberikan dua kali (dua preset) untuk mengukur memori saat dua job
// berjalan. Pakai tautan yang memang boleh diunduh.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/db"
	"github.com/irfanadwifangga/yt-to-mp3/internal/instance"
	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
)

const (
	historyJobs  = 10_000
	coldRuns     = 5
	apiRequests  = 300
	idleSettle   = 5 * time.Second
	startTimeout = 30 * time.Second
)

type result struct {
	aspect, target, got string
	ok                  *bool
}

func pass(v bool) *bool { return &v }

func main() {
	url := flag.String("url", "", "tautan YouTube untuk mengukur konversi (opsional, butuh jaringan)")
	bin := flag.String("bin", "", "binary yang diukur; kosong berarti build dari source")
	flag.Parse()

	if err := run(*bin, *url); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(bin, url string) error {
	work, err := os.MkdirTemp("", "yt2mp3-nfr-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	if bin == "" {
		bin = filepath.Join(work, "yt-to-mp3"+exeSuffix())
		fmt.Fprintln(os.Stderr, "==> build binary rilis")
		// Flag sama dengan build rilis: tanpa -s -w ukuran binary jauh lebih
		// besar dan angka yang dilaporkan menyesatkan.
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", bin, "./cmd/app")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build: %w", err)
		}
	}

	var results []result

	info, err := os.Stat(bin)
	if err != nil {
		return err
	}
	sizeMB := float64(info.Size()) / (1 << 20)
	results = append(results, result{"Ukuran binary (tanpa tool)", "< 25 MB",
		fmt.Sprintf("%.1f MB", sizeMB), pass(sizeMB < 25)})

	fmt.Fprintln(os.Stderr, "==> cold start")
	var starts []time.Duration
	for i := range coldRuns {
		inst, err := launch(bin, filepath.Join(work, fmt.Sprintf("cold-%d", i)))
		if err != nil {
			return err
		}
		starts = append(starts, inst.ready)
		if err := inst.stop(); err != nil {
			return err
		}
	}
	cold := median(starts)
	results = append(results, result{"Cold start sampai SPA tersaji (median " + strconv.Itoa(coldRuns) + "×)",
		"< 1.5 detik", cold.Round(time.Millisecond).String(), pass(cold < 1500*time.Millisecond)})

	fmt.Fprintf(os.Stderr, "==> semai %d job riwayat\n", historyJobs)
	dataRoot := filepath.Join(work, "main")
	if err := seedHistory(dataRoot, historyJobs); err != nil {
		return fmt.Errorf("semai riwayat: %w", err)
	}

	inst, err := launch(bin, dataRoot)
	if err != nil {
		return err
	}
	defer func() { _ = inst.stop() }()

	fmt.Fprintln(os.Stderr, "==> memori idle")
	time.Sleep(idleSettle)
	idleMB, err := rssMB(inst.cmd.Process.Pid)
	if err != nil {
		return err
	}
	results = append(results, result{"Memori idle", "< 60 MB", fmt.Sprintf("%.1f MB", idleMB), pass(idleMB < 60)})

	fmt.Fprintln(os.Stderr, "==> latensi API")
	endpoints := []string{"/api/health", "/api/settings", "/api/presets", "/api/jobs?limit=50"}
	var worst time.Duration
	var worstPath string
	for _, ep := range endpoints {
		p95, err := inst.latencyP95(ep, apiRequests)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "    %-22s p95 %s\n", ep, p95.Round(10*time.Microsecond))
		if p95 > worst {
			worst, worstPath = p95, ep
		}
	}
	results = append(results, result{"Latensi API non-download, p95 (terburuk: " + worstPath + ")",
		"< 50 ms", worst.Round(10 * time.Microsecond).String(), pass(worst < 50*time.Millisecond)})

	historyP95, err := inst.latencyP95("/api/jobs?limit=50&status=finished", apiRequests)
	if err != nil {
		return err
	}
	results = append(results, result{fmt.Sprintf("Riwayat %d job: p95 daftar", historyJobs),
		"tanpa degradasi terasa (< 50 ms)", historyP95.Round(10 * time.Microsecond).String(),
		pass(historyP95 < 50*time.Millisecond)})

	if url != "" {
		conv, err := inst.measureConversion(url)
		if err != nil {
			return fmt.Errorf("ukur konversi: %w", err)
		}
		results = append(results,
			result{"Memori saat 2 job berjalan (puncak proses aplikasi)", "< 200 MB",
				fmt.Sprintf("%.1f MB", conv.peakMB), pass(conv.peakMB < 200)},
			result{"CPU aplikasi sendiri, di luar tool (rata-rata, % satu core)", "< 5%",
				fmt.Sprintf("%.1f%%", conv.cpuPercent), pass(conv.cpuPercent < 5)},
			result{"Konversi 1 tautan (" + conv.duration + " sumber), 2 job paralel", "< 45 detik untuk 5 menit @192 kbps",
				conv.elapsed.Round(100 * time.Millisecond).String(), nil},
		)
	}

	printTable(results, url != "")
	return nil
}

/* Instance ---------------------------------------------------------------- */

type running struct {
	cmd   *exec.Cmd
	base  string
	token string
	ready time.Duration
	done  chan struct{}
}

// launch menjalankan binary pada direktori data terisolasi.
//
// Seluruh variabel yang menentukan direktori data per OS diarahkan ke root
// sementara, sehingga ukuran ini tidak pernah menyentuh instance pengguna.
func launch(bin, root string) (*running, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "-no-browser")
	cmd.Env = append(os.Environ(), isolatedEnv(root)...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	r := &running{cmd: cmd, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(r.done) }()

	runtimeFile := filepath.Join(dataDir(root), "runtime.json")
	client := &http.Client{Timeout: time.Second}

	for time.Since(start) < startTimeout {
		select {
		case <-r.done:
			return nil, errors.New("binary keluar sebelum siap")
		default:
		}
		if raw, err := os.ReadFile(runtimeFile); err == nil {
			var info instance.Info
			if json.Unmarshal(raw, &info) == nil && info.Port != 0 {
				// Siap berarti SPA benar-benar tersaji, bukan sekadar port
				// terbuka.
				base := fmt.Sprintf("http://127.0.0.1:%d", info.Port)
				if resp, err := client.Get(base + "/"); err == nil {
					body, _ := io.ReadAll(resp.Body)
					_ = resp.Body.Close()
					if resp.StatusCode == http.StatusOK && strings.Contains(string(body), `id="root"`) {
						r.base, r.token, r.ready = base, info.Token, time.Since(start)
						return r, nil
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = cmd.Process.Kill()
	return nil, errors.New("binary tidak siap dalam batas waktu")
}

func (r *running) request(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, r.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Session-Token", r.token)
	if method != http.MethodGet {
		req.Header.Set("Origin", r.base)
		req.Header.Set("Content-Type", "application/json")
	}
	return http.DefaultClient.Do(req)
}

func (r *running) stop() error {
	select {
	case <-r.done:
		return nil
	default:
	}
	if resp, err := r.request(http.MethodPost, "/api/shutdown", nil); err == nil {
		_ = resp.Body.Close()
	}
	select {
	case <-r.done:
		return nil
	case <-time.After(15 * time.Second):
		_ = r.cmd.Process.Kill()
		return errors.New("binary tidak berhenti setelah shutdown")
	}
}

func (r *running) latencyP95(path string, n int) (time.Duration, error) {
	// Beberapa permintaan pemanasan supaya koneksi keep-alive dan cache
	// halaman SQLite tidak ikut terhitung sebagai latensi.
	for range 10 {
		if resp, err := r.request(http.MethodGet, path, nil); err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}

	samples := make([]time.Duration, 0, n)
	for range n {
		start := time.Now()
		resp, err := r.request(http.MethodGet, path, nil)
		if err != nil {
			return 0, err
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("GET %s: %s", path, resp.Status)
		}
		samples = append(samples, time.Since(start))
	}
	return percentile(samples, 95), nil
}

type conversion struct {
	elapsed    time.Duration
	peakMB     float64
	cpuPercent float64
	duration   string
}

// measureConversion mengantrekan satu tautan dengan dua preset sekaligus,
// sehingga dua job berjalan paralel seperti batas bawaan aplikasi.
func (r *running) measureConversion(url string) (conversion, error) {
	fmt.Fprintln(os.Stderr, "==> konversi (butuh jaringan)")
	var c conversion

	resp, err := r.request(http.MethodPost, "/api/metadata", strings.NewReader(fmt.Sprintf(`{"url":%q}`, url)))
	if err != nil {
		return c, err
	}
	var meta struct {
		DurationMS int64 `json:"duration_ms"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&meta)
	_ = resp.Body.Close()
	c.duration = (time.Duration(meta.DurationMS) * time.Millisecond).Round(time.Second).String()

	pid := r.cmd.Process.Pid
	cpuStart, err := cpuSeconds(pid)
	if err != nil {
		return c, err
	}

	start := time.Now()
	var ids []string
	for _, preset := range []string{"mp3_standard", "mp3_high"} {
		resp, err := r.request(http.MethodPost, "/api/jobs",
			strings.NewReader(fmt.Sprintf(`{"url":%q,"preset_id":%q}`, url, preset)))
		if err != nil {
			return c, err
		}
		var job struct {
			ID string `json:"id"`
		}
		err = json.NewDecoder(resp.Body).Decode(&job)
		_ = resp.Body.Close()
		if err != nil || job.ID == "" {
			return c, fmt.Errorf("buat job %s: %s", preset, resp.Status)
		}
		ids = append(ids, job.ID)
	}

	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if mb, err := rssMB(pid); err == nil && mb > c.peakMB {
			c.peakMB = mb
		}

		finished := 0
		for _, id := range ids {
			resp, err := r.request(http.MethodGet, "/api/jobs/"+id, nil)
			if err != nil {
				return c, err
			}
			var job struct {
				Status    string `json:"status"`
				ErrorCode string `json:"error_code"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&job)
			_ = resp.Body.Close()
			switch job.Status {
			case "completed":
				finished++
			case "failed", "cancelled":
				return c, fmt.Errorf("job %s berakhir %s (%s)", id, job.Status, job.ErrorCode)
			}
		}
		if finished == len(ids) {
			c.elapsed = time.Since(start)
			cpuEnd, err := cpuSeconds(pid)
			if err != nil {
				return c, err
			}
			c.cpuPercent = (cpuEnd - cpuStart) / c.elapsed.Seconds() * 100
			return c, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return c, errors.New("konversi tidak selesai dalam 10 menit")
}

/* Semai riwayat ------------------------------------------------------------ */

// seedHistory mengisi database dengan job terminal lewat skema yang sama
// dengan aplikasi. Job dimasukkan sebagai completed, bukan queued: job antre
// akan dijalankan scheduler begitu aplikasi dibuka.
func seedHistory(root string, n int) error {
	ctx := context.Background()
	dbDir := filepath.Join(dataDir(root), "db")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return err
	}

	database, err := db.Open(ctx, filepath.Join(dbDir, "app.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return err
	}
	defer func() { _ = database.Close() }()

	if err := database.Migrate(ctx, version.Version); err != nil {
		return err
	}

	base := time.Now().UTC().Add(-time.Duration(n) * time.Minute)
	return database.InTx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO jobs (id, source_url, source_key, title, status, preset_id,
			                  filename_mode, progress, attempt_count, created_at, started_at, finished_at)
			VALUES (?, ?, ?, ?, 'completed', 'mp3_standard', 'title', 100, 1, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()

		for i := range n {
			key := fmt.Sprintf("%011d", i)
			created := base.Add(time.Duration(i) * time.Minute)
			if _, err := stmt.ExecContext(ctx,
				fmt.Sprintf("job_seed%08d", i),
				"https://www.youtube.com/watch?v="+key,
				"youtube:"+key,
				fmt.Sprintf("Lagu riwayat nomor %d", i),
				created.Format(time.RFC3339Nano),
				created.Add(time.Second).Format(time.RFC3339Nano),
				created.Add(40*time.Second).Format(time.RFC3339Nano),
			); err != nil {
				return err
			}
		}
		return nil
	})
}

/* Sistem operasi ----------------------------------------------------------- */

func isolatedEnv(root string) []string {
	return []string{
		"LOCALAPPDATA=" + root,
		"XDG_DATA_HOME=" + root,
		"HOME=" + root,
		"YT2MP3_OUTPUT_DIR=" + filepath.Join(root, "keluaran"),
	}
}

// dataDir mengikuti config.resolveDataDir untuk env hasil isolatedEnv.
func dataDir(root string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(root, "Library", "Application Support", version.AppName)
	}
	return filepath.Join(root, version.AppName)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// rssMB membaca memori resident proses. Pada Windows ini working set, angka
// yang sama dengan kolom Memory di Task Manager.
func rssMB(pid int) (float64, error) {
	if runtime.GOOS == "windows" {
		out, err := powershell(fmt.Sprintf("(Get-Process -Id %d).WorkingSet64", pid))
		if err != nil {
			return 0, err
		}
		b, err := strconv.ParseFloat(out, 64)
		return b / (1 << 20), err
	}
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	kb, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	return kb / 1024, err
}

// cpuSeconds membaca total waktu CPU proses, tidak termasuk proses anak.
func cpuSeconds(pid int) (float64, error) {
	if runtime.GOOS == "windows" {
		out, err := powershell(fmt.Sprintf("(Get-Process -Id %d).TotalProcessorTime.TotalSeconds", pid))
		if err != nil {
			return 0, err
		}
		return strconv.ParseFloat(strings.ReplaceAll(out, ",", "."), 64)
	}
	out, err := exec.Command("ps", "-o", "time=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	return parsePSTime(strings.TrimSpace(string(out)))
}

// parsePSTime mengurai format [[dd-]hh:]mm:ss[.cc] keluaran ps.
func parsePSTime(s string) (float64, error) {
	var days float64
	if d, rest, ok := strings.Cut(s, "-"); ok {
		v, err := strconv.ParseFloat(d, 64)
		if err != nil {
			return 0, err
		}
		days, s = v, rest
	}
	parts := strings.Split(s, ":")
	var total float64
	for _, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil {
			return 0, err
		}
		total = total*60 + v
	}
	return days*86400 + total, nil
}

func powershell(script string) (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	return strings.TrimSpace(string(out)), err
}

/* Statistik dan keluaran ---------------------------------------------------- */

func percentile(samples []time.Duration, p float64) time.Duration {
	slices.Sort(samples)
	idx := int(math.Ceil(p/100*float64(len(samples)))) - 1
	return samples[max(0, min(idx, len(samples)-1))]
}

func median(samples []time.Duration) time.Duration {
	return percentile(samples, 50)
}

func printTable(results []result, withConversion bool) {
	fmt.Printf("Diukur %s pada %s/%s, %d CPU, Go %s.\n\n",
		time.Now().Format("2006-01-02"), runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), runtime.Version())
	fmt.Println("| Aspek | Target | Hasil | Status |")
	fmt.Println("| --- | --- | --- | --- |")
	for _, r := range results {
		status := "catatan"
		if r.ok != nil {
			status = map[bool]string{true: "lolos", false: "**gagal**"}[*r.ok]
		}
		fmt.Printf("| %s | %s | %s | %s |\n", r.aspect, r.target, r.got, status)
	}
	if !withConversion {
		fmt.Println("\nMemori saat 2 job, CPU, dan waktu konversi tidak diukur; jalankan dengan -url.")
	}
}

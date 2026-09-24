// Command toolmanifest memperbarui internal/infrastructure/tools/manifest.json
// ke versi tool tertentu dan mengisi checksum SHA-256.
//
// Manifest bersifat fail-closed (ADR-033): build tanpa checksum menolak
// instalasi. Program ini satu-satunya cara resmi mengisinya, dan hasilnya
// di-commit sebagai perubahan tersendiri supaya bisa direview dan mudah
// di-bisect ketika pembaruan tool menyebabkan regresi.
//
// Setiap URL menunjuk rilis berversi tetap, tidak pernah tag bergulir seperti
// "latest": isi tag bergulir berganti, sehingga checksum yang di-pin
// terhadapnya pasti basi. Sumber dan alasannya dicatat di ADR-031.
//
// Pemakaian dari root repo:
//
//	go run ./scripts/toolmanifest                  versi terbaru
//	go run ./scripts/toolmanifest -ffmpeg 9.0      versi FFmpeg tertentu
//	go run ./scripts/toolmanifest -verify          ikut unduh dan periksa isi arsip
//
// Variabel GITHUB_TOKEN, bila ada, dipakai untuk menaikkan batas rate API.
package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/infrastructure/tools"
)

const manifestPath = "internal/infrastructure/tools/manifest.json"

const manifestNote = "Dibuat oleh scripts/toolmanifest; jangan disunting manual. " +
	"Checksum kosong menolak instalasi (ADR-033)."

var client = &http.Client{Timeout: 20 * time.Minute}

func main() {
	var (
		ytdlpVersion  = flag.String("ytdlp", "", "tag rilis yt-dlp; kosong berarti terbaru")
		ffmpegVersion = flag.String("ffmpeg", "", "versi rilis FFmpeg; kosong berarti rilis terbaru GyanD")
		verify        = flag.Bool("verify", false, "unduh setiap berkas untuk memeriksa checksum dan isi arsip (ratusan MB)")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx, *ytdlpVersion, *ffmpegVersion, *verify); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, ytdlpVersion, ffmpegVersion string, verify bool) error {
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf("jalankan dari root repo: %w", err)
	}

	fmt.Println("==> yt-dlp")
	ytdlp, err := ytdlpTool(ctx, ytdlpVersion)
	if err != nil {
		return fmt.Errorf("yt-dlp: %w", err)
	}

	fmt.Println("==> FFmpeg")
	ffmpeg, err := ffmpegTool(ctx, ffmpegVersion)
	if err != nil {
		return fmt.Errorf("ffmpeg: %w", err)
	}

	m := tools.Manifest{
		Schema: 2,
		Note:   manifestNote,
		Tools:  map[string]tools.Tool{"yt-dlp": ytdlp, "ffmpeg": ffmpeg},
	}

	if verify {
		fmt.Println("==> verifikasi")
		if err := verifyManifest(ctx, m); err != nil {
			return err
		}
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}

	fmt.Printf("\nSelesai: yt-dlp %s, FFmpeg %s.\nReview diff pada %s lalu commit.\n",
		ytdlp.Version, ffmpeg.Version, manifestPath)
	return nil
}

/* yt-dlp ----------------------------------------------------------------- */

func ytdlpTool(ctx context.Context, tag string) (tools.Tool, error) {
	rel, err := githubRelease(ctx, "yt-dlp/yt-dlp", tag)
	if err != nil {
		return tools.Tool{}, err
	}
	fmt.Println("    rilis:", rel.TagName)

	// yt-dlp menerbitkan checksum resmi, jadi binary tidak perlu diunduh.
	sumsAsset, ok := rel.asset("SHA2-256SUMS")
	if !ok {
		return tools.Tool{}, errors.New("rilis tidak memuat SHA2-256SUMS")
	}
	text, err := fetchText(ctx, sumsAsset.URL)
	if err != nil {
		return tools.Tool{}, err
	}
	sums := tools.ParseSHA256Sums(text)

	builds := map[string]tools.Build{}
	for platform, a := range tools.YTDLPAssets {
		asset, ok := rel.asset(a.Asset)
		if !ok {
			return tools.Tool{}, fmt.Errorf("aset %s tidak ada", a.Asset)
		}
		sum := sums[a.Asset]
		if sum == "" {
			return tools.Tool{}, fmt.Errorf("%s tidak tercantum di SHA2-256SUMS", a.Asset)
		}
		// Digest dari GitHub dihitung sendiri oleh GitHub saat aset diunggah.
		// Ketidakcocokan dengan berkas checksum hulu berarti ada yang salah
		// dan tidak boleh di-pin.
		if asset.sha256() != "" && asset.sha256() != sum {
			return tools.Tool{}, fmt.Errorf("%s: digest GitHub %s berbeda dengan SHA2-256SUMS %s",
				a.Asset, asset.sha256(), sum)
		}
		builds[platform] = tools.Build{Downloads: []tools.Download{{
			URL: asset.URL, SHA256: sum, Archive: tools.ArchiveNone, Extract: []string{a.Target},
		}}}
		fmt.Printf("    %-14s %s…\n", platform, sum[:12])
	}

	return tools.Tool{
		Version: rel.TagName,
		Source:  "https://github.com/yt-dlp/yt-dlp",
		Builds:  builds,
	}, nil
}

/* FFmpeg ----------------------------------------------------------------- */

// riedlPlatforms memetakan platform Go ke penamaan martin-riedl.de.
var riedlPlatforms = map[string][2]string{
	"linux/amd64":  {"linux", "amd64"},
	"linux/arm64":  {"linux", "arm64"},
	"darwin/amd64": {"macos", "amd64"},
	"darwin/arm64": {"macos", "arm64"},
}

const riedlBase = "https://ffmpeg.martin-riedl.de"

// ffmpegTool mem-pin versi FFmpeg yang sama untuk seluruh platform.
//
// Versi mengikuti rilis GyanD karena build Windows yang paling jarang
// tersedia; build Linux dan macOS dicari dengan versi yang sama. Manifest
// dengan versi berbeda per platform membuat laporan bug sulit dikaitkan,
// jadi versi yang belum tersedia di salah satu sumber membuat program gagal
// alih-alih mencampur versi.
func ffmpegTool(ctx context.Context, version string) (tools.Tool, error) {
	rel, err := githubRelease(ctx, "GyanD/codexffmpeg", version)
	if err != nil {
		return tools.Tool{}, err
	}
	version = rel.TagName
	fmt.Println("    versi:", version)

	name := tools.FFmpegWindowsAsset(version)
	asset, ok := rel.asset(name)
	if !ok {
		return tools.Tool{}, fmt.Errorf("aset %s tidak ada di rilis GyanD", name)
	}
	winSum := asset.sha256()
	if winSum == "" {
		fmt.Println("    digest GitHub kosong, menghitung sendiri", name)
		if winSum, err = hashURL(ctx, asset.URL); err != nil {
			return tools.Tool{}, err
		}
	}

	builds := map[string]tools.Build{
		"windows/amd64": {Downloads: []tools.Download{{
			URL: asset.URL, SHA256: winSum, Archive: tools.ArchiveZip,
			Extract: []string{"ffmpeg.exe", "ffprobe.exe"},
		}}},
	}
	fmt.Printf("    %-14s %s…\n", "windows/amd64", winSum[:12])

	// martin-riedl.de menerbitkan ffmpeg dan ffprobe sebagai zip terpisah,
	// masing-masing dengan berkas .sha256.
	for platform, p := range riedlPlatforms {
		dir, err := riedlReleaseDir(ctx, p[0], p[1], version)
		if err != nil {
			return tools.Tool{}, fmt.Errorf("%s: %w", platform, err)
		}

		var downloads []tools.Download
		for _, bin := range []string{"ffmpeg", "ffprobe"} {
			url := dir + "/" + bin + ".zip"
			sum, err := riedlChecksum(ctx, url+".sha256", bin+".zip")
			if err != nil {
				return tools.Tool{}, fmt.Errorf("%s: %w", platform, err)
			}
			downloads = append(downloads, tools.Download{
				URL: url, SHA256: sum, Archive: tools.ArchiveZip, Extract: []string{bin},
			})
		}
		builds[platform] = tools.Build{Downloads: downloads}
		fmt.Printf("    %-14s %s… %s…\n", platform, downloads[0].SHA256[:12], downloads[1].SHA256[:12])
	}

	return tools.Tool{
		Version: version,
		Source:  "https://github.com/GyanD/codexffmpeg (Windows); " + riedlBase + " (Linux, macOS)",
		Builds:  builds,
	}, nil
}

// riedlReleaseDir mencari direktori unduhan untuk sebuah versi rilis.
//
// Situs itu tidak punya API JSON; riwayat rilis dibaca dari halaman HTML.
// Bila strukturnya berubah, program gagal dengan jelas alih-alih menebak.
func riedlReleaseDir(ctx context.Context, osName, arch, version string) (string, error) {
	page, err := fetchText(ctx, fmt.Sprintf("%s/info/history/%s/%s/release", riedlBase, osName, arch))
	if err != nil {
		return "", err
	}

	pattern := regexp.MustCompile(fmt.Sprintf(`/info/detail/%s/%s/([0-9]+_([^"/]+))"`,
		regexp.QuoteMeta(osName), regexp.QuoteMeta(arch)))

	var seen []string
	for _, m := range pattern.FindAllStringSubmatch(page, -1) {
		if m[2] == version {
			return fmt.Sprintf("%s/download/%s/%s/%s", riedlBase, osName, arch, m[1]), nil
		}
		seen = append(seen, m[2])
	}
	if len(seen) == 0 {
		return "", errors.New("riwayat rilis martin-riedl.de tidak terbaca; struktur halaman mungkin berubah")
	}
	return "", fmt.Errorf("versi %s belum tersedia (tersedia: %s); pilih versi lain dengan -ffmpeg",
		version, strings.Join(seen[:min(len(seen), 5)], ", "))
}

func riedlChecksum(ctx context.Context, url, wantName string) (string, error) {
	text, err := fetchText(ctx, url)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(text)
	if len(fields) < 2 || fields[1] != wantName {
		return "", fmt.Errorf("format %s tidak dikenali: %q", url, text)
	}
	sum := strings.ToLower(fields[0])
	if raw, err := hex.DecodeString(sum); err != nil || len(raw) != sha256.Size {
		return "", fmt.Errorf("checksum %s tidak valid: %q", url, fields[0])
	}
	return sum, nil
}

/* Verifikasi ------------------------------------------------------------- */

// verifyManifest mengunduh setiap berkas, mencocokkan checksum, dan
// memastikan berkas yang akan diekstrak benar-benar ada di dalam arsip.
// Checksum yang benar atas arsip yang isinya salah tetap menggagalkan
// instalasi, dan itu baru ketahuan di mesin pengguna.
func verifyManifest(ctx context.Context, m tools.Manifest) error {
	checked := map[string]bool{}

	for _, toolName := range sortedKeys(m.Tools) {
		tool := m.Tools[toolName]
		for _, platform := range sortedKeys(tool.Builds) {
			for _, d := range tool.Builds[platform].Downloads {
				if checked[d.URL] {
					continue
				}
				checked[d.URL] = true
				fmt.Printf("    %s %s\n", toolName, path.Base(d.URL))
				if err := verifyDownload(ctx, d); err != nil {
					return fmt.Errorf("%s/%s: %w", toolName, platform, err)
				}
			}
		}
	}
	return nil
}

func verifyDownload(ctx context.Context, d tools.Download) error {
	tmp, err := os.CreateTemp("", "toolmanifest-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	resp, err := get(ctx, d.URL)
	if err != nil {
		return err
	}
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}

	if got := hex.EncodeToString(h.Sum(nil)); got != d.SHA256 {
		return fmt.Errorf("checksum %s tidak cocok: dapat %s, manifest %s", d.URL, got, d.SHA256)
	}

	if d.Archive == tools.ArchiveZip {
		zr, err := zip.NewReader(tmp, size)
		if err != nil {
			return fmt.Errorf("buka zip %s: %w", d.URL, err)
		}
		for _, want := range d.Extract {
			found := slices.ContainsFunc(zr.File, func(f *zip.File) bool {
				return !f.FileInfo().IsDir() && strings.EqualFold(path.Base(f.Name), want)
			})
			if !found {
				return fmt.Errorf("%s tidak ada di dalam %s", want, d.URL)
			}
		}
	}
	return nil
}

/* HTTP ------------------------------------------------------------------- */

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}

func (a ghAsset) sha256() string {
	return strings.ToLower(strings.TrimPrefix(a.Digest, "sha256:"))
}

func (r ghRelease) asset(name string) (ghAsset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return ghAsset{}, false
}

func githubRelease(ctx context.Context, repo, tag string) (ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	if tag != "" {
		url = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repo, tag)
	}

	resp, err := get(ctx, url)
	if err != nil {
		return ghRelease{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return ghRelease{}, fmt.Errorf("decode %s: %w", url, err)
	}
	return rel, nil
}

func fetchText(ctx context.Context, url string) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(body), err
}

func hashURL(ctx context.Context, url string) (string, error) {
	resp, err := get(ctx, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, resp.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "yt-to-mp3-toolmanifest")
	if strings.HasPrefix(url, "https://api.github.com/") {
		req.Header.Set("Accept", "application/vnd.github+json")
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

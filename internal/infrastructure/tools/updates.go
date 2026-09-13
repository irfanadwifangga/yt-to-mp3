package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

const (
	defaultGitHubAPI      = "https://api.github.com"
	defaultGitHubDownload = "https://github.com"

	ytdlpRepo  = "yt-dlp/yt-dlp"
	ffmpegRepo = "GyanD/codexffmpeg"

	// checkTimeout membatasi satu pertanyaan versi terbaru ke GitHub.
	checkTimeout = 30 * time.Second
	// maxSumsBytes membatasi berkas checksum; aslinya hanya beberapa KB.
	maxSumsBytes = 1 << 20
)

// YTDLPAsset memetakan satu platform ke nama aset rilis yt-dlp dan nama
// berkas terpasangnya.
type YTDLPAsset struct {
	Asset  string
	Target string
}

// YTDLPAssets dipakai bersama oleh pembaruan dari aplikasi dan
// scripts/toolmanifest, supaya keduanya tidak pernah memilih aset berbeda.
// yt-dlp_macos adalah universal binary, jadi dipakai kedua arsitektur macOS.
var YTDLPAssets = map[string]YTDLPAsset{
	"windows/amd64": {Asset: "yt-dlp.exe", Target: "yt-dlp.exe"},
	"linux/amd64":   {Asset: "yt-dlp_linux", Target: "yt-dlp"},
	"linux/arm64":   {Asset: "yt-dlp_linux_aarch64", Target: "yt-dlp"},
	"darwin/amd64":  {Asset: "yt-dlp_macos", Target: "yt-dlp"},
	"darwin/arm64":  {Asset: "yt-dlp_macos", Target: "yt-dlp"},
}

// Tag rilis divalidasi sebelum dipakai menyusun URL, supaya jawaban API yang
// tidak terduga tidak bisa membelokkan unduhan ke path lain.
var (
	ytdlpTag  = regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}(\.\d+)?$`)
	ffmpegTag = regexp.MustCompile(`^\d+(\.\d+){0,3}$`)
)

// ParseSHA256Sums mengurai berkas checksum berformat sha256sum.
func ParseSHA256Sums(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			out[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
		}
	}
	return out
}

// LatestVersion menanyakan versi rilis terbaru sebuah tool ke sumbernya.
//
// ffprobe mengikuti ffmpeg karena keduanya berasal dari rilis yang sama.
func (m *Manager) LatestVersion(ctx context.Context, name string) (string, error) {
	var repo string
	var pattern *regexp.Regexp
	switch name {
	case YTDLP:
		repo, pattern = ytdlpRepo, ytdlpTag
	case FFmpeg, FFprobe:
		repo, pattern = ffmpegRepo, ffmpegTag
	default:
		return "", fmt.Errorf("tool %q tidak dikenal", name)
	}

	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()

	body, err := m.get(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", m.githubAPIBase(), repo), 1<<20)
	if err != nil {
		return "", err
	}

	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", fmt.Errorf("urai rilis %s: %w", repo, err)
	}
	if !pattern.MatchString(rel.TagName) {
		return "", fmt.Errorf("tag rilis %s tidak dikenal: %q", repo, rel.TagName)
	}
	return rel.TagName, nil
}

// InstallLatestYTDLP memasang yt-dlp rilis terbaru dan mengembalikan
// versinya.
//
// Berbeda dari Install, checksum tidak berasal dari manifest yang di-pin di
// binary, melainkan dari SHA2-256SUMS yang diterbitkan di rilis yang sama,
// seperti yang dilakukan `yt-dlp -U`. Ini pengecualian yang disengaja dari
// ADR-033 khusus yt-dlp: yt-dlp rusak mengikuti perubahan di sisi YouTube
// jauh lebih cepat daripada siklus rilis aplikasi. Hanya dijalankan atas
// permintaan eksplisit pengguna.
func (m *Manager) InstallLatestYTDLP(ctx context.Context) (string, error) {
	asset, ok := YTDLPAssets[platformKey()]
	if !ok {
		return "", domain.NewError(domain.CodeToolManifest, domain.ClassLocal,
			fmt.Sprintf("yt-dlp tidak menerbitkan build untuk %s", platformKey()))
	}

	tag, err := m.LatestVersion(ctx, YTDLP)
	if err != nil {
		return "", domain.WrapError(domain.CodeToolUpdateCheck, domain.ClassTransient,
			"baca rilis terbaru yt-dlp", err)
	}

	base := fmt.Sprintf("%s/%s/releases/download/%s/", m.githubDownloadBase(), ytdlpRepo, tag)
	sumsCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	raw, err := m.get(sumsCtx, base+"SHA2-256SUMS", maxSumsBytes)
	cancel()
	if err != nil {
		return "", domain.WrapError(domain.CodeToolInstallFailed, domain.ClassTransient,
			"unduh SHA2-256SUMS", err)
	}
	sum := ParseSHA256Sums(string(raw))[asset.Asset]
	if sum == "" {
		return "", domain.NewError(domain.CodeToolInstallFailed, domain.ClassPermanent,
			fmt.Sprintf("%s tidak tercantum di SHA2-256SUMS rilis %s", asset.Asset, tag))
	}

	d := Download{URL: base + asset.Asset, SHA256: sum, Archive: ArchiveNone, Extract: []string{asset.Target}}

	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return "", domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"buat direktori tools", err)
	}
	m.log.Info("memperbarui yt-dlp", "versi", tag, "url", d.URL)

	archive, err := m.download(ctx, d)
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(archive) }()

	if err := m.extract(archive, d); err != nil {
		return "", err
	}

	m.Invalidate()
	m.log.Info("yt-dlp diperbarui", "versi", tag)
	return tag, nil
}

func (m *Manager) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "yt-to-mp3")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func (m *Manager) githubAPIBase() string {
	if m.githubAPI != "" {
		return m.githubAPI
	}
	return defaultGitHubAPI
}

func (m *Manager) githubDownloadBase() string {
	if m.githubDownload != "" {
		return m.githubDownload
	}
	return defaultGitHubDownload
}

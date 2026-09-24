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

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
	"github.com/irfanadwifangga/yt-to-mp3/internal/version"
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

// FFmpegWindowsAsset adalah nama arsip build essentials GyanD untuk satu
// versi. Dipakai bersama oleh pembaruan dari aplikasi dan
// scripts/toolmanifest, supaya keduanya tidak pernah memilih arsip berbeda.
// Build essentials sudah memuat libmp3lame dan libx264 dan jauh lebih kecil
// daripada full build.
func FFmpegWindowsAsset(version string) string {
	return fmt.Sprintf("ffmpeg-%s-essentials_build.zip", version)
}

// ffmpegUpdatePlatform adalah satu-satunya platform yang dapat memperbarui
// FFmpeg dari aplikasi. Rilis GyanD punya API GitHub dengan checksum per
// aset; sumber Linux dan macOS hanya punya halaman HTML yang strukturnya
// bisa berubah kapan saja, jadi di sana FFmpeg tetap mengikuti manifest.
const ffmpegUpdatePlatform = "windows/amd64"

// Tag rilis divalidasi sebelum dipakai menyusun URL, supaya jawaban API yang
// tidak terduga tidak bisa membelokkan unduhan ke path lain.
var (
	ytdlpTag  = regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}(\.\d+)?$`)
	ffmpegTag = regexp.MustCompile(`^\d+(\.\d+){0,3}$`)
	// Rilis prarilis seperti v1.2.0-rc.1 tidak pernah dijawab
	// /releases/latest, jadi hanya bentuk stabil yang diterima.
	appTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	// Digest aset GitHub berbentuk "sha256:<64 hex>".
	sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
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
	case version.AppName:
		repo, pattern = version.Repo, appTag
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
	// Tag aplikasi berbentuk v1.2.3, sedangkan versi yang disematkan
	// GoReleaser tanpa awalan v.
	return strings.TrimPrefix(rel.TagName, "v"), nil
}

// CanUpdate melaporkan apakah tool dapat diperbarui ke rilis terbarunya
// dari aplikasi di platform ini. ffprobe tidak diperbarui sendiri; ia ikut
// arsip FFmpeg.
func (m *Manager) CanUpdate(name string) bool {
	switch name {
	case YTDLP:
		_, ok := YTDLPAssets[m.platform()]
		return ok
	case FFmpeg:
		return m.platform() == ffmpegUpdatePlatform
	default:
		return false
	}
}

// InstallLatest memasang rilis terbaru sebuah tool dan mengembalikan
// versinya. Hanya dijalankan atas permintaan eksplisit pengguna.
func (m *Manager) InstallLatest(ctx context.Context, name string) (string, error) {
	switch name {
	case YTDLP:
		return m.InstallLatestYTDLP(ctx)
	case FFmpeg:
		return m.InstallLatestFFmpeg(ctx)
	default:
		return "", domain.NewError(domain.CodeInternal, domain.ClassLocal,
			fmt.Sprintf("%s tidak dapat diperbarui dari aplikasi", name))
	}
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
	asset, ok := YTDLPAssets[m.platform()]
	if !ok {
		return "", domain.NewError(domain.CodeToolManifest, domain.ClassLocal,
			fmt.Sprintf("yt-dlp tidak menerbitkan build untuk %s", m.platform()))
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
	if err := m.installRelease(ctx, YTDLP, tag, d); err != nil {
		return "", err
	}
	return tag, nil
}

// InstallLatestFFmpeg memasang build essentials GyanD terbaru, berisi
// ffmpeg dan ffprobe, dan mengembalikan versinya.
//
// Pengecualian kedua dari ADR-033, khusus Windows: checksum berasal dari
// digest SHA-256 yang dihitung GitHub untuk aset rilis itu, sumber yang
// sama dengan yang dipakai scripts/toolmanifest saat mem-pin manifest.
// Aset tanpa digest ditolak alih-alih di-hash sendiri, karena hash atas
// unduhan yang sama tidak memverifikasi apa pun. Salinan terkelola
// didahulukan discovery, jadi FFmpeg dari PATH tidak disentuh.
func (m *Manager) InstallLatestFFmpeg(ctx context.Context) (string, error) {
	if m.platform() != ffmpegUpdatePlatform {
		return "", domain.NewError(domain.CodeToolManifest, domain.ClassLocal,
			fmt.Sprintf("FFmpeg untuk %s mengikuti rilis aplikasi", m.platform()))
	}

	tag, err := m.LatestVersion(ctx, FFmpeg)
	if err != nil {
		return "", domain.WrapError(domain.CodeToolUpdateCheck, domain.ClassTransient,
			"baca rilis terbaru FFmpeg", err)
	}

	relCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	body, err := m.get(relCtx, fmt.Sprintf("%s/repos/%s/releases/tags/%s", m.githubAPIBase(), ffmpegRepo, tag), 1<<20)
	cancel()
	if err != nil {
		return "", domain.WrapError(domain.CodeToolUpdateCheck, domain.ClassTransient,
			"baca aset rilis FFmpeg", err)
	}
	var rel struct {
		Assets []struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", domain.WrapError(domain.CodeToolUpdateCheck, domain.ClassTransient,
			"urai aset rilis FFmpeg", err)
	}

	name := FFmpegWindowsAsset(tag)
	var sum string
	for _, a := range rel.Assets {
		if a.Name == name {
			sum = strings.ToLower(strings.TrimPrefix(a.Digest, "sha256:"))
		}
	}
	if !sha256Hex.MatchString(sum) {
		return "", domain.NewError(domain.CodeToolInstallFailed, domain.ClassPermanent,
			fmt.Sprintf("rilis FFmpeg %s tidak menyertakan checksum untuk %s", tag, name))
	}

	// URL disusun dari tag yang sudah divalidasi, bukan dari
	// browser_download_url, sama dengan jalur yt-dlp.
	d := Download{
		URL:     fmt.Sprintf("%s/%s/releases/download/%s/%s", m.githubDownloadBase(), ffmpegRepo, tag, name),
		SHA256:  sum,
		Archive: ArchiveZip,
		Extract: []string{"ffmpeg.exe", "ffprobe.exe"},
	}
	if err := m.installRelease(ctx, FFmpeg, tag, d); err != nil {
		return "", err
	}
	return tag, nil
}

// installRelease mengunduh, memverifikasi, dan memasang satu arsip rilis
// sambil melaporkan kemajuannya atas nama tool.
func (m *Manager) installRelease(ctx context.Context, name, tag string, d Download) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return domain.WrapError(domain.CodeToolInstallFailed, domain.ClassLocal,
			"buat direktori tools", err)
	}
	m.log.Info("memperbarui tool", "tool", name, "versi", tag, "url", d.URL)

	defer m.clearProgress(name)
	m.setProgress(name, application.ToolProgress{Phase: application.ToolPhaseDownloading, Step: 1, Steps: 1})
	archive, err := m.download(ctx, d, func(done, total int64) {
		m.setProgress(name, application.ToolProgress{
			Phase: application.ToolPhaseDownloading, Step: 1, Steps: 1,
			DoneBytes: done, TotalBytes: total,
		})
	})
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(archive) }()

	m.setProgress(name, application.ToolProgress{Phase: application.ToolPhaseExtracting, Step: 1, Steps: 1})
	if err := m.extract(archive, d); err != nil {
		return err
	}

	m.Invalidate()
	m.log.Info("tool diperbarui", "tool", name, "versi", tag)
	return nil
}

// platform mengembalikan kunci platform, atau pengganti yang dipasang test.
func (m *Manager) platform() string {
	if m.platformOverride != "" {
		return m.platformOverride
	}
	return platformKey()
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

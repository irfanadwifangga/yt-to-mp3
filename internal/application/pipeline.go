package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// Bobot fase pada progress keseluruhan. Unduhan mendominasi karena di
// situlah waktu sebenarnya dihabiskan; transcode audio jauh lebih cepat
// daripada mengambil berkasnya. Lihat docs planning "Model progress".
const (
	pctResolved    = 5
	pctDownloaded  = 70
	pctConverted   = 95
	phaseResolving = "resolving"
	phaseDownload  = "downloading"
	phaseConvert   = "converting"
	phaseVerify    = "verifying"
)

// Batas waktu diturunkan dari durasi media, bukan konstanta: podcast tiga
// jam akan selalu kena timeout tetap. Lihat ADR-025.
const (
	resolveTimeout  = 60 * time.Second
	verifyTimeout   = 60 * time.Second
	minDownloadTime = 10 * time.Minute
	minConvertTime  = 5 * time.Minute
	downloadFactor  = 3
	convertFactor   = 1
)

// diskSafetyFactor menyisakan ruang untuk berkas sumber, hasil, dan sisa
// sementara yang hidup bersamaan di puncak pemakaian.
const diskSafetyFactor = 1.5

// assumedSourceKbps adalah perkiraan bitrate sumber ketika ukuran unduhan
// belum diketahui. Opus 160 kbps adalah format audio terbaik yang lazim
// disajikan YouTube.
const assumedSourceKbps = 160

// DownloadRequest adalah permintaan unduhan.
type DownloadRequest struct {
	SourceKey string
	TempDir   string
	Timeout   time.Duration
}

// DownloadOutcome menunjuk berkas hasil unduhan.
type DownloadOutcome struct {
	AudioPath string
	CoverPath string
}

// Downloader mengambil audio sumber.
type Downloader interface {
	Download(ctx context.Context, req DownloadRequest, onProgress func(*float64)) (*DownloadOutcome, error)
}

// TranscodeRequest adalah permintaan konversi.
type TranscodeRequest struct {
	AudioPath  string
	CoverPath  string
	OutputPath string
	Preset     *domain.Preset
	Media      *domain.MediaInfo
	Timeout    time.Duration
}

// Transcoder mengubah audio sumber menjadi format keluaran.
type Transcoder interface {
	Transcode(ctx context.Context, req TranscodeRequest, onProgress func(*float64)) error
}

// Verifier memastikan berkas hasil layak dianggap sukses.
type Verifier interface {
	Verify(ctx context.Context, path string, expected time.Duration) error
}

// OutputStore mengelola berkas sementara dan memberi akses ke direktori
// keluaran.
//
// Antarmuka ini hanya memakai tipe dasar supaya layer application tidak
// perlu mengenal paket filesystem mana pun.
type OutputStore interface {
	TempDirFor(jobID string) (string, error)
	RemoveTempDir(jobID string) error

	// Output mengembalikan snapshot direktori keluaran saat ini. Direktori
	// itu dapat diganti pengguna kapan saja, jadi sebuah job wajib mengambil
	// snapshot sekali di awal dan memakainya sampai commit.
	Output() OutputTarget
}

// OutputTarget adalah satu direktori keluaran yang dipakai sebuah job dari
// awal sampai commit.
type OutputTarget interface {
	Dir() string
	CommitTempFile(jobID, ext string) (string, error)
	ReservePath(filename string) (path, name string, release func(), err error)
	CommitPath(tmpPath, finalPath string) error
	FreeSpace() (uint64, error)
}

// FilenameBuilder menyusun nama berkas keluaran.
type FilenameBuilder interface {
	Build(mode domain.FilenameMode, info *domain.MediaInfo, ext string) string
}

// JobCloser menutup job sukses beserta berkasnya dalam satu transaksi.
type JobCloser interface {
	Complete(ctx context.Context, jobID string, from domain.JobStatus, f *domain.File, ev domain.Event) error
	UpdateProgress(ctx context.Context, jobID string, percent *float64, phase string) error
	// SetTitle mengisi judul job yang dibuat tanpa judul; judul yang sudah
	// ada tidak ditimpa.
	SetTitle(ctx context.Context, jobID, title string) error
}

// Pipeline menjalankan satu job dari metadata sampai berkas final.
type Pipeline struct {
	repo       JobRepository
	closer     JobCloser
	presets    PresetLister
	cache      MediaCache
	resolver   MediaResolver
	downloader Downloader
	transcoder Transcoder
	verifier   Verifier
	store      OutputStore
	naming     FilenameBuilder
	events     EventPublisher
	log        *slog.Logger
}

// PipelineDeps mengumpulkan dependensi Pipeline.
type PipelineDeps struct {
	Repo       JobRepository
	Closer     JobCloser
	Presets    PresetLister
	Cache      MediaCache
	Resolver   MediaResolver
	Downloader Downloader
	Transcoder Transcoder
	Verifier   Verifier
	Store      OutputStore
	Naming     FilenameBuilder
	Events     EventPublisher
	Log        *slog.Logger
}

// NewPipeline membuat runner pipeline.
func NewPipeline(d PipelineDeps) *Pipeline {
	return &Pipeline{
		repo: d.Repo, closer: d.Closer, presets: d.Presets, cache: d.Cache,
		resolver: d.Resolver, downloader: d.Downloader, transcoder: d.Transcoder,
		verifier: d.Verifier, store: d.Store, naming: d.Naming,
		events: d.Events, log: d.Log,
	}
}

// Run menjalankan seluruh fase satu job.
//
// Job masuk ke sini sudah berstatus resolving: scheduler yang mengambilnya
// dari antrean. Kontraknya, Run wajib menutup job ke status terminal saat
// sukses; kegagalan cukup dikembalikan sebagai error dan scheduler yang
// menutupnya.
func (p *Pipeline) Run(ctx context.Context, job *domain.Job) error {
	tempDir, err := p.store.TempDirFor(job.ID)
	if err != nil {
		return domain.WrapError(domain.CodeOutputWriteFailed, domain.ClassLocal,
			"siapkan direktori kerja", err)
	}
	// Pembersihan berjalan setelah seluruh proses anak dituai oleh masing
	// masing tahap, sehingga tidak ada berkas yang masih terkunci.
	defer func() {
		if err := p.store.RemoveTempDir(job.ID); err != nil {
			p.log.Warn("bersihkan temp gagal", "job", job.ID, "error", err)
		}
	}()

	// Satu snapshot untuk seluruh job: reservasi nama, berkas sementara, dan
	// commit harus berada di direktori yang sama walau pengguna mengganti
	// folder keluaran di tengah jalan.
	out := p.store.Output()

	preset, err := p.presets.Get(ctx, job.PresetID)
	if err != nil {
		return domain.NewError(domain.CodeInternal, domain.ClassPermanent,
			fmt.Sprintf("preset %s tidak dapat dibaca", job.PresetID))
	}

	media, err := p.resolveMedia(ctx, job)
	if err != nil {
		return err
	}

	// Job yang diantrekan tanpa analisis lebih dulu belum punya judul, dan
	// tanpa ini riwayatnya selamanya menampilkan source key.
	if job.Title == "" && media.Title != "" {
		if err := p.closer.SetTitle(ctx, job.ID, media.Title); err != nil {
			p.log.Warn("simpan judul gagal", "job", job.ID, "error", err)
		}
		job.Title = media.Title
	}
	p.setPhase(ctx, job.ID, pctResolved, phaseResolving)

	if err := p.preflightDisk(out, media, preset); err != nil {
		return err
	}

	// Unduhan
	if err := p.transition(ctx, job.ID, domain.StatusResolving, domain.StatusDownloading); err != nil {
		return err
	}
	downloaded, err := p.downloader.Download(ctx, DownloadRequest{
		SourceKey: job.SourceKey,
		TempDir:   tempDir,
		Timeout:   phaseTimeout(media.Duration, downloadFactor, minDownloadTime),
	}, func(pct *float64) {
		p.publishProgress(job.ID, phaseDownload, scale(pct, pctResolved, pctDownloaded))
	})
	if err != nil {
		return err
	}
	p.setPhase(ctx, job.ID, pctDownloaded, phaseDownload)

	// Konversi
	if err := p.transition(ctx, job.ID, domain.StatusDownloading, domain.StatusConverting); err != nil {
		return err
	}

	ext := "." + preset.Format
	filename := p.naming.Build(job.FilenameMode, media, ext)
	finalPath, finalName, release, err := out.ReservePath(filename)
	if err != nil {
		return domain.WrapError(domain.CodeOutputWriteFailed, domain.ClassLocal,
			"pesan nama berkas", err)
	}
	// Reservasi dilepas bila job gagal; setelah commit berhasil penanda
	// sudah tergantikan berkas asli dan pelepasan tidak berdampak.
	committed := false
	defer func() {
		if !committed {
			release()
		}
	}()

	outTmp, err := out.CommitTempFile(job.ID, ext)
	if err != nil {
		return domain.WrapError(domain.CodeOutputWriteFailed, domain.ClassLocal,
			"siapkan berkas sementara", err)
	}
	defer func() {
		if !committed {
			_ = os.Remove(outTmp)
		}
	}()

	err = p.transcoder.Transcode(ctx, TranscodeRequest{
		AudioPath:  downloaded.AudioPath,
		CoverPath:  downloaded.CoverPath,
		OutputPath: outTmp,
		Preset:     preset,
		Media:      media,
		Timeout:    phaseTimeout(media.Duration, convertFactor, minConvertTime),
	}, func(pct *float64) {
		p.publishProgress(job.ID, phaseConvert, scale(pct, pctDownloaded, pctConverted))
	})
	if err != nil {
		return err
	}
	p.setPhase(ctx, job.ID, pctConverted, phaseConvert)

	// Verifikasi
	if err := p.transition(ctx, job.ID, domain.StatusConverting, domain.StatusVerifying); err != nil {
		return err
	}

	verifyCtx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	if err := p.verifier.Verify(verifyCtx, outTmp, media.Duration); err != nil {
		return err
	}

	size, sum, err := hashFile(outTmp)
	if err != nil {
		return domain.WrapError(domain.CodeVerifyFailed, domain.ClassLocal,
			"hitung checksum hasil", err)
	}

	// Commit terakhir: sebelum titik ini tidak ada apa pun di direktori
	// keluaran selain penanda kosong.
	if err := out.CommitPath(outTmp, finalPath); err != nil {
		return domain.WrapError(domain.CodeOutputWriteFailed, domain.ClassLocal,
			"pindahkan hasil", err)
	}
	committed = true

	fileID, err := newFileID()
	if err != nil {
		return err
	}
	file := &domain.File{
		ID: fileID, JobID: job.ID, Path: finalPath, Filename: finalName,
		MIME: mimeFor(preset.Format), SizeBytes: size, SHA256: sum,
	}

	if err := p.closer.Complete(ctx, job.ID, domain.StatusVerifying, file,
		domain.Event{Type: domain.EventDone, Payload: StreamEvent{
			Type: StreamDone, Status: domain.StatusCompleted,
		}.PayloadJSON()}); err != nil {
		return err
	}

	p.log.Info("job selesai", "job", job.ID, "berkas", finalName, "ukuran", size)
	p.events.Publish(StreamEvent{
		JobID: job.ID, Type: StreamDone, Status: domain.StatusCompleted,
	})
	return nil
}

// resolveMedia mengambil metadata, memakai cache bila masih segar.
func (p *Pipeline) resolveMedia(ctx context.Context, job *domain.Job) (*domain.MediaInfo, error) {
	if info, ok, err := p.cache.Get(ctx, job.SourceKey); err == nil && ok {
		if info.IsLive {
			return nil, domain.NewError(domain.CodeLiveNotSupported, domain.ClassPermanent,
				"sumber adalah siaran langsung")
		}
		return info, nil
	}

	resolveCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	info, err := p.resolver.Resolve(resolveCtx, job.SourceKey)
	if err != nil {
		return nil, err
	}
	if err := p.cache.Upsert(ctx, info, ""); err != nil {
		p.log.Warn("simpan cache metadata gagal", "job", job.ID, "error", err)
	}
	return info, nil
}

// preflightDisk menolak job yang jelas tidak akan muat.
//
// Gagal di awal jauh lebih baik daripada gagal pada 95 persen setelah
// menghabiskan bandwidth dan waktu pengguna.
func (p *Pipeline) preflightDisk(out OutputTarget, media *domain.MediaInfo, preset *domain.Preset) error {
	free, err := out.FreeSpace()
	if err != nil {
		p.log.Warn("baca ruang kosong gagal, preflight dilewati", "error", err)
		return nil // jangan menggagalkan job hanya karena tidak bisa mengukur
	}

	seconds := media.Duration.Seconds()
	if seconds <= 0 {
		return nil // durasi tidak diketahui, tidak ada dasar perhitungan
	}

	kbps := assumedSourceKbps
	if preset.BitrateKbps != nil {
		kbps += *preset.BitrateKbps
	}
	needed := uint64(seconds * float64(kbps) * 1000 / 8 * diskSafetyFactor)

	if free < needed {
		return domain.NewError(domain.CodeDiskFull, domain.ClassLocal,
			fmt.Sprintf("butuh sekitar %d MB, tersedia %d MB",
				needed/(1<<20), free/(1<<20)))
	}
	return nil
}

// phaseTimeout menurunkan batas waktu dari durasi media.
func phaseTimeout(duration time.Duration, factor int, minimum time.Duration) time.Duration {
	scaled := duration * time.Duration(factor)
	if scaled < minimum {
		return minimum
	}
	return scaled
}

// scale memetakan persentase fase ke rentangnya pada progress keseluruhan.
//
// Nil tetap nil: fase yang tidak tahu totalnya menghasilkan progress
// indeterminate, bukan nol.
func scale(pct *float64, from, to float64) *float64 {
	if pct == nil {
		return nil
	}
	v := from + (*pct/100)*(to-from)
	return &v
}

func (p *Pipeline) transition(ctx context.Context, jobID string, from, to domain.JobStatus) error {
	return p.repo.Transition(ctx, jobID, from, to, domain.Event{
		Type:    domain.EventState,
		Payload: StreamEvent{Type: StreamState, Status: to}.PayloadJSON(),
	})
}

// setPhase menyimpan kemajuan pada batas fase saja; nilai live mengalir
// lewat SSE tanpa menyentuh database.
func (p *Pipeline) setPhase(ctx context.Context, jobID string, pct float64, phase string) {
	if err := p.closer.UpdateProgress(ctx, jobID, &pct, phase); err != nil {
		p.log.Warn("simpan progress gagal", "job", jobID, "error", err)
	}
	p.events.Publish(StreamEvent{
		JobID: jobID, Type: StreamProgress, Phase: phase, Percent: &pct,
	})
}

func (p *Pipeline) publishProgress(jobID, phase string, pct *float64) {
	p.events.Publish(StreamEvent{
		JobID: jobID, Type: StreamProgress, Phase: phase, Percent: pct,
	})
}

// hashFile menghitung ukuran dan SHA-256 berkas hasil.
func hashFile(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}

func newFileID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("buat id berkas: %w", err)
	}
	return "file_" + hex.EncodeToString(buf), nil
}

// mimeFor memetakan format preset ke tipe MIME.
func mimeFor(format string) string {
	switch format {
	case "mp3":
		return "audio/mpeg"
	case "m4a":
		return "audio/mp4"
	case "opus":
		return "audio/opus"
	case "flac":
		return "audio/flac"
	case "wav":
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}

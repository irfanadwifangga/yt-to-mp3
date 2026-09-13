package api_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// newDiscardLogger membuat logger yang membuang seluruh output agar keluaran
// test tetap bersih.
func newDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeTools menggantikan tool manager sungguhan supaya test tidak pernah
// menyentuh filesystem maupun jaringan.
type fakeTools struct {
	status     map[string]application.ToolStatus
	installed  []string
	installErr error
	checks     int
	checkErr   error
	updated    []string
	checkedAt  *time.Time
}

func (f *fakeTools) CheckUpdates(context.Context) error {
	f.checks++
	if f.checkErr != nil {
		return f.checkErr
	}
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	f.checkedAt = &now
	return nil
}

func (f *fakeTools) Update(_ context.Context, name string) error {
	f.updated = append(f.updated, name)
	return nil
}

func (f *fakeTools) CheckedAt() *time.Time { return f.checkedAt }

func newFakeTools() *fakeTools {
	return &fakeTools{
		status: map[string]application.ToolStatus{
			"yt-dlp":  {Name: "yt-dlp", Available: true, Version: "2026.01.01", Source: "managed"},
			"ffmpeg":  {Name: "ffmpeg", Available: false},
			"ffprobe": {Name: "ffprobe", Available: false},
		},
	}
}

func (f *fakeTools) StatusAll(context.Context) map[string]application.ToolStatus {
	return f.status
}

func (f *fakeTools) Install(_ context.Context, name string) error {
	if f.installErr != nil {
		return f.installErr
	}
	f.installed = append(f.installed, name)
	return nil
}

// fakeResolver mencatat source key yang diminta sehingga test dapat
// memastikan validasi URL terjadi sebelum resolver dipanggil.
type fakeResolver struct {
	info    *domain.MediaInfo
	err     error
	calls   int
	lastKey string
}

func newFakeResolver() *fakeResolver {
	return &fakeResolver{
		info: &domain.MediaInfo{
			SourceKey:   "youtube:dQw4w9WgXcQ",
			SourceURL:   "https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			Title:       "Judul Contoh",
			Uploader:    "Channel Contoh",
			DurationMS:  212000,
			SourceCodec: "opus",
			SampleRate:  48000,
		},
	}
}

func (f *fakeResolver) Resolve(_ context.Context, sourceKey string) (*domain.MediaInfo, error) {
	f.calls++
	f.lastKey = sourceKey
	if f.err != nil {
		return nil, f.err
	}
	return f.info, nil
}

// fakeCache adalah cache metadata in-memory; selalu meleset kecuali test
// mengisinya lebih dulu.
type fakeCache struct {
	items map[string]*domain.MediaInfo
	puts  int
}

func newFakeCache() *fakeCache {
	return &fakeCache{items: map[string]*domain.MediaInfo{}}
}

func (c *fakeCache) Get(_ context.Context, key string) (*domain.MediaInfo, bool, error) {
	info, ok := c.items[key]
	return info, ok, nil
}

func (c *fakeCache) Upsert(_ context.Context, info *domain.MediaInfo, _ string) error {
	c.puts++
	c.items[info.SourceKey] = info
	return nil
}

// fakePresets menggantikan repository preset.
type fakePresets struct {
	presets []domain.Preset
	err     error
}

func newFakePresets() *fakePresets {
	rate := 48000
	bitrate := 192
	return &fakePresets{presets: []domain.Preset{{
		ID: "mp3_standard", Label: "Standard", Format: "mp3", Codec: "libmp3lame",
		Mode: "cbr", BitrateKbps: &bitrate, SampleRate: &rate, Channels: 2,
	}}}
}

func (p *fakePresets) List(context.Context, bool) ([]domain.Preset, error) {
	return p.presets, p.err
}

func (p *fakePresets) Get(_ context.Context, id string) (*domain.Preset, error) {
	for i := range p.presets {
		if p.presets[i].ID == id {
			return &p.presets[i], nil
		}
	}
	return nil, domain.NewError(domain.CodeInternal, domain.ClassLocal, "tidak ada")
}

// fakeJobRepo adalah penyimpanan job in-memory untuk test lapisan HTTP.
type fakeJobRepo struct {
	mu     sync.Mutex
	jobs   map[string]*domain.Job
	order  []string
	events map[string][]domain.Event
}

func newFakeJobRepo() *fakeJobRepo {
	return &fakeJobRepo{
		jobs:   map[string]*domain.Job{},
		events: map[string][]domain.Event{},
	}
}

func (r *fakeJobRepo) Create(_ context.Context, j *domain.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.jobs {
		if existing.SourceKey == j.SourceKey && existing.PresetID == j.PresetID &&
			existing.Status.IsActive() {
			return domain.NewError(domain.CodeDuplicateActive, domain.ClassPermanent, "duplikat")
		}
	}
	copied := *j
	r.jobs[j.ID] = &copied
	r.order = append(r.order, j.ID)
	return nil
}

func (r *fakeJobRepo) Get(_ context.Context, id string) (*domain.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return nil, domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent, "tidak ada")
	}
	copied := *j
	return &copied, nil
}

func (r *fakeJobRepo) List(_ context.Context, q application.JobListQuery) ([]*domain.Job, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Job
	for i := len(r.order) - 1; i >= 0; i-- {
		j := r.jobs[r.order[i]]
		if q.Status != "" && j.Status != q.Status {
			continue
		}
		if q.SourceKey != "" && j.SourceKey != q.SourceKey {
			continue
		}
		if (q.Scope == application.ScopeActive && !j.Status.IsActive()) ||
			(q.Scope == application.ScopeFinished && !j.Status.IsTerminal()) {
			continue
		}
		copied := *j
		out = append(out, &copied)
	}
	return out, "", nil
}

func (r *fakeJobRepo) Transition(
	_ context.Context, id string, from, to domain.JobStatus, ev domain.Event,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent, "tidak ada")
	}
	if j.Status != from || !domain.CanTransition(from, to) {
		return domain.ErrInvalidTransition(from, to)
	}
	j.Status = to
	if ev.Type != "" {
		ev.JobID = id
		ev.Seq = int64(len(r.events[id]) + 1)
		r.events[id] = append(r.events[id], ev)
	}
	return nil
}

func (r *fakeJobRepo) Fail(
	_ context.Context, id string, _ domain.JobStatus, code domain.ErrorCode, detail string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent, "tidak ada")
	}
	j.Status = domain.StatusFailed
	j.ErrorCode = code
	j.ErrorMessage = detail
	return nil
}

func (r *fakeJobRepo) Requeue(
	_ context.Context, id string, _ domain.JobStatus, code domain.ErrorCode, detail string, retryAt time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent, "tidak ada")
	}
	j.Status = domain.StatusQueued
	j.AttemptCount++
	j.ErrorCode = code
	j.ErrorMessage = detail
	j.RetryAt = &retryAt
	return nil
}

func (r *fakeJobRepo) ClaimNextQueued(context.Context) (*domain.Job, error) { return nil, nil }
func (r *fakeJobRepo) SweepNonTerminal(context.Context) (int, error)        { return 0, nil }

func (r *fakeJobRepo) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[id]; !ok {
		return domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent, "tidak ada")
	}
	delete(r.jobs, id)
	return nil
}

func (r *fakeJobRepo) Events(_ context.Context, jobID string, afterSeq int64) ([]domain.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Event
	for _, ev := range r.events[jobID] {
		if ev.Seq > afterSeq {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (r *fakeJobRepo) CountActive(context.Context) (int, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var active, queued int
	for _, j := range r.jobs {
		switch {
		case j.Status == domain.StatusQueued:
			queued++
		case j.Status.IsActive():
			active++
		}
	}
	return active, queued, nil
}

// fakeCanceller mencatat permintaan pembatalan.
type fakeCanceller struct {
	cancelled []string
	err       error
}

func (c *fakeCanceller) Cancel(_ context.Context, jobID string) error {
	if c.err != nil {
		return c.err
	}
	c.cancelled = append(c.cancelled, jobID)
	return nil
}

// fakeFiles menggantikan repository berkas.
type fakeFiles struct {
	byID    map[string]*domain.File
	byJob   map[string]string
	missing []string
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{byID: map[string]*domain.File{}, byJob: map[string]string{}}
}

func (f *fakeFiles) add(file *domain.File) {
	f.byID[file.ID] = file
	f.byJob[file.JobID] = file.ID
}

func (f *fakeFiles) Get(_ context.Context, id string) (*domain.File, error) {
	file, ok := f.byID[id]
	if !ok {
		return nil, domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent, "tidak ada")
	}
	return file, nil
}

func (f *fakeFiles) GetByJob(_ context.Context, jobID string) (*domain.File, error) {
	id, ok := f.byJob[jobID]
	if !ok {
		return nil, domain.NewError(domain.CodeJobNotFound, domain.ClassPermanent, "tidak ada")
	}
	return f.byID[id], nil
}

func (f *fakeFiles) RefsByJobs(_ context.Context, jobIDs []string) (map[string]application.FileRef, error) {
	out := map[string]application.FileRef{}
	for _, id := range jobIDs {
		if fileID, ok := f.byJob[id]; ok {
			out[id] = application.FileRef{ID: fileID, Filename: f.byID[fileID].Filename}
		}
	}
	return out, nil
}

func (f *fakeFiles) MarkMissing(_ context.Context, id string) error {
	f.missing = append(f.missing, id)
	return nil
}

// fakeRevealer mencatat permintaan buka lokasi berkas.
type fakeRevealer struct {
	revealed []string
	err      error
}

func (r *fakeRevealer) Reveal(path string) error {
	if r.err != nil {
		return r.err
	}
	r.revealed = append(r.revealed, path)
	return nil
}

func (f *fakeTools) Progress() map[string]application.ToolProgress {
	return map[string]application.ToolProgress{}
}

// AppUpdate memakai nilai tetap supaya bentuk app_update terkunci di golden.
func (f *fakeTools) AppUpdate() application.AppUpdate {
	return application.AppUpdate{
		Current: "1.0.0", Latest: "1.1.0", UpdateAvailable: true,
		ReleaseURL: "https://github.com/irfanadwifangga/yt-to-mp3/releases/latest",
	}
}

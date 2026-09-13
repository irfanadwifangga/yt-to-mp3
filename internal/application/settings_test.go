package application_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

type memSettings struct {
	values map[string]string
	putErr error
	puts   int
}

func (m *memSettings) All(context.Context) (map[string]string, error) {
	out := make(map[string]string, len(m.values))
	for k, v := range m.values {
		out[k] = v
	}
	return out, nil
}

func (m *memSettings) Put(_ context.Context, values map[string]string) error {
	if m.putErr != nil {
		return m.putErr
	}
	m.puts++
	for k, v := range values {
		m.values[k] = v
	}
	return nil
}

type presetList struct{}

func (presetList) List(context.Context, bool) ([]domain.Preset, error) {
	return []domain.Preset{{ID: "mp3_standard"}, {ID: "mp3_max"}}, nil
}

func (presetList) Get(_ context.Context, id string) (*domain.Preset, error) {
	return &domain.Preset{ID: id}, nil
}

func newSettings(t *testing.T) (*application.SettingsService, *memSettings, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "awal")
	store := &memSettings{values: map[string]string{}}
	svc := application.NewSettingsService(store, presetList{}, map[string]string{
		application.KeyOutputDir:       dir,
		application.KeyMaxConcurrent:   "2",
		application.KeyMaxQueueDepth:   "50",
		application.KeyDefaultPreset:   "mp3_standard",
		application.KeyFilenameMode:    "title",
		application.KeyToolUpdateCheck: "true",
		application.KeyIdleShutdown:    "30",
		application.KeyLogLevel:        "info",
	})
	if err := svc.Load(context.Background()); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return svc, store, dir
}

func wantInvalidSetting(t *testing.T, err error, key string) {
	t.Helper()
	var derr *domain.Error
	if !errors.As(err, &derr) {
		t.Fatalf("error = %v, mau *domain.Error", err)
	}
	if derr.Code != domain.CodeInvalidSetting {
		t.Errorf("kode = %s, mau %s", derr.Code, domain.CodeInvalidSetting)
	}
	if derr.Details["key"] != key {
		t.Errorf("details.key = %q, mau %q", derr.Details["key"], key)
	}
}

// Penerap dipanggil sebelum nilai disimpan, sehingga folder baru langsung
// dipakai tanpa restart.
func TestSettingsApplierDipanggilLaluDisimpan(t *testing.T) {
	svc, store, _ := newSettings(t)
	target := filepath.Join(t.TempDir(), "musik")

	var got string
	svc.SetApplier(application.KeyOutputDir, func(v string) error {
		got = v
		return nil
	})

	if err := svc.Update(context.Background(), map[string]string{
		application.KeyOutputDir: target,
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got != target {
		t.Errorf("applier menerima %q, mau %q", got, target)
	}
	if store.values[application.KeyOutputDir] != target {
		t.Errorf("tersimpan %q, mau %q", store.values[application.KeyOutputDir], target)
	}
}

// Nilai yang ditolak penerap tidak boleh tersimpan, supaya database tidak
// pernah menunjuk folder yang terbukti tidak bisa dipakai.
func TestSettingsApplierMenolakTidakMenyimpan(t *testing.T) {
	svc, store, _ := newSettings(t)
	svc.SetApplier(application.KeyOutputDir, func(string) error {
		return errors.New("tidak dapat ditulisi")
	})

	err := svc.Update(context.Background(), map[string]string{
		application.KeyOutputDir: filepath.Join(t.TempDir(), "terkunci"),
	})
	wantInvalidSetting(t, err, application.KeyOutputDir)

	if store.puts != 0 {
		t.Errorf("tersimpan %d kali, mau 0", store.puts)
	}
}

// Bila penyimpanan gagal setelah nilai terlanjur diterapkan, komponen yang
// berjalan harus kembali ke nilai lama. Tanpa rollback, aplikasi menulis ke
// folder yang tidak tercatat di database.
func TestSettingsRollbackBilaPenyimpananGagal(t *testing.T) {
	svc, store, awal := newSettings(t)
	store.putErr = errors.New("database terkunci")
	baru := filepath.Join(t.TempDir(), "baru")

	var calls []string
	svc.SetApplier(application.KeyOutputDir, func(v string) error {
		calls = append(calls, v)
		return nil
	})

	if err := svc.Update(context.Background(), map[string]string{
		application.KeyOutputDir: baru,
	}); err == nil {
		t.Fatal("Update() seharusnya gagal")
	}

	if len(calls) != 2 || calls[0] != baru || calls[1] != awal {
		t.Errorf("urutan penerapan = %v, mau [%s %s]", calls, baru, awal)
	}
}

func TestSettingsApplierTidakDipanggilBilaNilaiSama(t *testing.T) {
	svc, _, awal := newSettings(t)

	called := false
	svc.SetApplier(application.KeyOutputDir, func(string) error {
		called = true
		return nil
	})

	if err := svc.Update(context.Background(), map[string]string{
		application.KeyOutputDir: awal,
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if called {
		t.Error("applier dipanggil walau nilainya tidak berubah")
	}
}

// Nilai idle baru harus langsung terbaca monitor tanpa restart.
func TestSettingsIdleBerlakuSeketika(t *testing.T) {
	svc, _, _ := newSettings(t)

	if got := svc.Live().IdleShutdownMinutes; got != 30 {
		t.Fatalf("bawaan = %d, mau 30", got)
	}
	if err := svc.Update(context.Background(), map[string]string{
		application.KeyIdleShutdown: "0",
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got := svc.Live().IdleShutdownMinutes; got != 0 {
		t.Errorf("setelah diubah = %d, mau 0", got)
	}

	err := svc.Update(context.Background(), map[string]string{
		application.KeyIdleShutdown: "1441",
	})
	wantInvalidSetting(t, err, application.KeyIdleShutdown)
}

func TestSettingsOutputDirHarusAbsolut(t *testing.T) {
	svc, _, _ := newSettings(t)

	err := svc.Update(context.Background(), map[string]string{
		application.KeyOutputDir: filepath.Join("relatif", "folder"),
	})
	wantInvalidSetting(t, err, application.KeyOutputDir)
}

// Setelan tanpa implementasi tidak boleh tampil sebagai kontrol di UI, dan
// setelan yang punya penerap tidak boleh ditandai perlu restart.
func TestSettingsListMencerminkanPerilakuSebenarnya(t *testing.T) {
	svc, _, _ := newSettings(t)

	views, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	byKey := map[string]application.SettingView{}
	for _, v := range views {
		byKey[v.Key] = v
	}

	if _, ok := byKey[application.KeyToolUpdateCheck]; ok {
		t.Errorf("%s belum diimplementasikan tetapi ditampilkan", application.KeyToolUpdateCheck)
	}

	idle, ok := byKey[application.KeyIdleShutdown]
	if !ok {
		t.Errorf("%s sudah diimplementasikan tetapi tidak ditampilkan", application.KeyIdleShutdown)
	}
	if idle.RequiresRestart {
		t.Error("idle_shutdown_minutes dibaca ulang setiap pemeriksaan, tidak boleh ditandai perlu restart")
	}

	out := byKey[application.KeyOutputDir]
	if out.Kind != "path" {
		t.Errorf("output_dir kind = %q, mau path", out.Kind)
	}
	if out.RequiresRestart {
		t.Error("output_dir berlaku seketika, tidak boleh ditandai perlu restart")
	}
	if byKey[application.KeyLogLevel].RequiresRestart {
		t.Error("log_level berlaku seketika, tidak boleh ditandai perlu restart")
	}
	if !byKey[application.KeyMaxConcurrent].RequiresRestart {
		t.Error("max_concurrent_jobs dibaca saat startup dan harus ditandai perlu restart")
	}
}

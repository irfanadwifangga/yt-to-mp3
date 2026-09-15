package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		installed, latest string
		want              bool
	}{
		{"2026.08.19", "2026.09.01", true},
		{"2026.09.01", "2026.09.01", false},
		{"2026.09.01.1", "2026.09.01", false},
		{"2026.09.01", "2026.09.01.1", true},
		{"9.0.1-essentials_build-www.gyan.dev", "9.0.1", false},
		{"9.0.1-full_build-www.gyan.dev", "9.1", true},
		{"n7.1", "9.0.1", true},
		{"4.4.2-0ubuntu0.22.04.1", "9.0.1", true},
		// Snapshot tidak bisa dibandingkan; jangan tawarkan "pembaruan".
		{"N-126498-gc2bb5aa8d8", "9.0.1", false},
		{"", "9.0.1", false},
		{"9.0.1", "", false},
	}
	for _, tc := range tests {
		if got := application.IsNewerVersion(tc.installed, tc.latest); got != tc.want {
			t.Errorf("IsNewerVersion(%q, %q) = %v, mau %v", tc.installed, tc.latest, got, tc.want)
		}
	}
}

type fakeUpdateSource struct {
	statuses  map[string]application.ToolStatus
	latest    map[string]string
	latestErr error
	checks    int
	installed string
}

func (f *fakeUpdateSource) StatusAll(context.Context) map[string]application.ToolStatus {
	out := map[string]application.ToolStatus{}
	for k, v := range f.statuses {
		out[k] = v
	}
	return out
}

func (f *fakeUpdateSource) Install(context.Context, string) error { return nil }

func (f *fakeUpdateSource) LatestVersion(_ context.Context, name string) (string, error) {
	f.checks++
	if f.latestErr != nil {
		return "", f.latestErr
	}
	return f.latest[name], nil
}

func (f *fakeUpdateSource) InstallLatestYTDLP(context.Context) (string, error) {
	f.installed = f.latest["yt-dlp"]
	f.statuses["yt-dlp"] = application.ToolStatus{Name: "yt-dlp", Available: true, Version: f.installed}
	return f.installed, nil
}

type memUpdateStore struct {
	state application.ToolUpdateState
	saves int
}

func (m *memUpdateStore) Load() (application.ToolUpdateState, error) { return m.state, nil }
func (m *memUpdateStore) Save(s application.ToolUpdateState) error {
	m.state = s
	m.saves++
	return nil
}

func newToolFixture(enabled *bool) (*application.ToolService, *fakeUpdateSource, *memUpdateStore, *time.Time) {
	src := &fakeUpdateSource{
		statuses: map[string]application.ToolStatus{
			"yt-dlp":  {Name: "yt-dlp", Available: true, Version: "2026.08.19"},
			"ffmpeg":  {Name: "ffmpeg", Available: true, Version: "9.0.1-essentials_build-www.gyan.dev"},
			"ffprobe": {Name: "ffprobe", Available: true, Version: "9.0.1-essentials_build-www.gyan.dev"},
		},
		latest: map[string]string{"yt-dlp": "2026.09.01", "ffmpeg": "9.0.1"},
	}
	store := &memUpdateStore{}
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	svc := application.NewToolService(src, store, func() bool { return *enabled }, discard())
	svc.SetClock(func() time.Time { return now })
	return svc, src, store, &now
}

func TestToolServiceMenandaiPembaruan(t *testing.T) {
	enabled := true
	svc, _, store, _ := newToolFixture(&enabled)
	ctx := context.Background()

	if st := svc.StatusAll(ctx)["yt-dlp"]; st.UpdateAvailable {
		t.Error("pembaruan ditandai sebelum pernah dicek")
	}

	if err := svc.CheckUpdates(ctx); err != nil {
		t.Fatalf("CheckUpdates() error = %v", err)
	}
	statuses := svc.StatusAll(ctx)
	if st := statuses["yt-dlp"]; !st.UpdateAvailable || st.Latest != "2026.09.01" {
		t.Errorf("yt-dlp = %+v, mau ada pembaruan", st)
	}
	if st := statuses["ffprobe"]; st.UpdateAvailable || st.Latest != "9.0.1" {
		t.Errorf("ffprobe = %+v, mau mengikuti ffmpeg tanpa pembaruan", st)
	}
	if store.saves != 1 || svc.CheckedAt() == nil {
		t.Errorf("hasil cek tidak tersimpan: saves=%d checked=%v", store.saves, svc.CheckedAt())
	}
}

func TestToolServiceJatuhTempoMingguan(t *testing.T) {
	enabled := true
	svc, _, _, now := newToolFixture(&enabled)

	if !svc.Due() {
		t.Fatal("belum pernah dicek tetapi tidak jatuh tempo")
	}
	if err := svc.CheckUpdates(context.Background()); err != nil {
		t.Fatal(err)
	}
	if svc.Due() {
		t.Error("jatuh tempo lagi tepat setelah dicek")
	}

	*now = now.Add(application.ToolUpdateInterval)
	if !svc.Due() {
		t.Error("tidak jatuh tempo setelah seminggu")
	}

	enabled = false
	if svc.Due() {
		t.Error("jatuh tempo walau setelan dimatikan")
	}
}

// Offline tidak boleh mengosongkan hasil lama maupun memajukan waktu cek.
func TestToolServiceCekGagalTidakMenimpaHasilLama(t *testing.T) {
	enabled := true
	svc, src, _, _ := newToolFixture(&enabled)
	ctx := context.Background()

	if err := svc.CheckUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	first := *svc.CheckedAt()

	src.latestErr = errors.New("offline")
	err := svc.CheckUpdates(ctx)
	var derr *domain.Error
	if !errors.As(err, &derr) || derr.Code != domain.CodeToolUpdateCheck {
		t.Fatalf("CheckUpdates() offline = %v, mau %s", err, domain.CodeToolUpdateCheck)
	}
	if got := svc.StatusAll(ctx)["yt-dlp"].Latest; got != "2026.09.01" {
		t.Errorf("versi terbaru lama hilang: %q", got)
	}
	if !svc.CheckedAt().Equal(first) {
		t.Error("waktu cek maju walau cek gagal")
	}
}

func TestToolServiceUpdateHanyaYTDLP(t *testing.T) {
	enabled := true
	svc, src, _, _ := newToolFixture(&enabled)
	ctx := context.Background()

	if err := svc.Update(ctx, "ffmpeg"); err == nil {
		t.Error("ffmpeg seharusnya tidak bisa diperbarui dari aplikasi")
	}

	if err := svc.CheckUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Update(ctx, "yt-dlp"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if src.installed != "2026.09.01" {
		t.Errorf("terpasang = %q", src.installed)
	}
	if svc.StatusAll(ctx)["yt-dlp"].UpdateAvailable {
		t.Error("pembaruan masih ditandai setelah dipasang")
	}
}

func (f *fakeUpdateSource) Progress() map[string]application.ToolProgress { return nil }

func TestToolServiceAppUpdate(t *testing.T) {
	const releases = "https://github.com/irfanadwifangga/yt-to-mp3/releases/latest"
	enabled := true
	svc, src, _, _ := newToolFixture(&enabled)
	src.latest[application.AppUpdateKey] = "1.2.0"
	ctx := context.Background()

	svc.SetApp("1.1.0", releases)
	if u := svc.AppUpdate(); u.UpdateAvailable || u.Latest != "" {
		t.Errorf("sebelum dicek = %+v, mau tanpa informasi", u)
	}

	if err := svc.CheckUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	u := svc.AppUpdate()
	if !u.UpdateAvailable || u.Latest != "1.2.0" || u.Current != "1.1.0" || u.ReleaseURL != releases {
		t.Errorf("setelah dicek = %+v, mau pembaruan ke 1.2.0", u)
	}

	tests := []struct {
		current string
		want    bool
	}{
		{"1.2.0", false},
		{"1.3.0", false},
		// Build dari source dan snapshot CI tidak ditawari rilis resmi.
		{"0.1.0-dev", false},
		{"1.1.1-snapshot", false},
		{"", false},
	}
	for _, tc := range tests {
		svc.SetApp(tc.current, releases)
		if got := svc.AppUpdate().UpdateAvailable; got != tc.want {
			t.Errorf("versi %q: update_available = %v, mau %v", tc.current, got, tc.want)
		}
	}
}

// Hasil cek lama yang belum memuat versi aplikasi tidak boleh membuat
// aplikasi menunggu seminggu untuk tahu versi terbarunya.
func TestToolServiceCekSegeraBilaVersiAplikasiBelumDiketahui(t *testing.T) {
	enabled := true
	svc, src, _, _ := newToolFixture(&enabled)
	ctx := context.Background()

	// Cek pertama hanya berhasil untuk tool, seperti hasil dari versi lama.
	if err := svc.CheckUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	svc.SetApp("0.1.0", "https://github.com/irfanadwifangga/yt-to-mp3/releases/latest")
	if !svc.Due() {
		t.Fatal("tidak jatuh tempo walau versi aplikasi belum pernah diketahui")
	}

	src.latest[application.AppUpdateKey] = "0.1.0"
	if err := svc.CheckUpdates(ctx); err != nil {
		t.Fatal(err)
	}
	if svc.Due() {
		t.Error("masih jatuh tempo setelah versi aplikasi diketahui")
	}

	// Setelan yang dimatikan tetap dihormati.
	src.latest[application.AppUpdateKey] = ""
	svc.SetApp("0.1.0", "")
	enabled = false
	if svc.Due() {
		t.Error("jatuh tempo walau cek pembaruan dimatikan")
	}
}

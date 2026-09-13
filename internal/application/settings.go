package application

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// Kunci setelan yang dapat diubah pengguna. Set ini tertutup: kunci di luar
// daftar ditolak, sehingga UI tidak dapat menulis konfigurasi sembarangan.
const (
	KeyOutputDir       = "output_dir"
	KeyMaxConcurrent   = "max_concurrent_jobs"
	KeyMaxQueueDepth   = "max_queue_depth"
	KeyDefaultPreset   = "default_preset_id"
	KeyFilenameMode    = "filename_mode"
	KeyToolUpdateCheck = "tool_update_check"
	KeyIdleShutdown    = "idle_shutdown_minutes"
	KeyLogLevel        = "log_level"
)

// SettingsStore menyimpan override konfigurasi.
type SettingsStore interface {
	All(ctx context.Context) (map[string]string, error)
	Put(ctx context.Context, values map[string]string) error
}

// LiveSettings adalah nilai yang dibaca ulang setiap permintaan.
type LiveSettings struct {
	DefaultPresetID string
	FilenameMode    domain.FilenameMode
	MaxQueueDepth   int
	ToolUpdateCheck bool

	// IdleShutdownMinutes nol berarti aplikasi tidak berhenti sendiri.
	IdleShutdownMinutes int
}

// SettingView adalah satu baris setelan beserta metadata untuk UI.
type SettingView struct {
	Key     string   `json:"key"`
	Value   string   `json:"value"`
	Kind    string   `json:"kind"` // string | int | bool | enum | path
	Options []string `json:"options,omitempty"`

	// RequiresRestart menandai setelan yang baru berlaku setelah aplikasi
	// dijalankan ulang, karena nilainya dibaca sekali saat startup.
	// Menampilkannya sebagai seolah-olah langsung berlaku akan menyesatkan.
	RequiresRestart bool `json:"requires_restart"`
}

// restartRequired adalah setelan yang dibaca sekali saat startup.
//
// Jumlah job paralel menentukan kapasitas slot scheduler yang disusun saat
// aplikasi mulai. Direktori keluaran dan tingkat log tidak termasuk karena
// keduanya punya applier yang memasang nilai baru seketika.
var restartRequired = map[string]bool{
	KeyMaxConcurrent: true,
}

// notImplemented adalah setelan yang sudah divalidasi dan boleh diisi lewat
// config.json, tetapi belum punya implementasi. Sengaja tidak ditampilkan
// di UI: kontrol yang tidak berpengaruh apa pun menjanjikan perilaku yang
// tidak pernah terjadi.
var notImplemented = map[string]bool{
	KeyToolUpdateCheck: true,
}

// Applier menerapkan nilai setelan ke komponen yang sedang berjalan.
type Applier func(value string) error

// SettingsService membaca dan menulis setelan.
type SettingsService struct {
	store    SettingsStore
	presets  PresetLister
	defaults map[string]string

	mu   sync.RWMutex
	live LiveSettings

	// updateMu menyerialkan Update supaya penerapan dan rollback dua
	// perubahan yang berbarengan tidak saling menimpa.
	updateMu sync.Mutex
	appliers map[string]Applier
}

// NewSettingsService membuat use case setelan.
//
// defaults berisi nilai bawaan hasil konfigurasi startup; override dari
// database ditumpangkan di atasnya.
func NewSettingsService(
	store SettingsStore, presets PresetLister, defaults map[string]string,
) *SettingsService {
	return &SettingsService{
		store:    store,
		presets:  presets,
		defaults: defaults,
		appliers: map[string]Applier{},
	}
}

// SetApplier mendaftarkan penerap untuk sebuah kunci.
//
// Penerap dipanggil sebelum nilai disimpan. Bila ia menolak, perubahan
// dibatalkan dan tidak ada yang tersimpan, sehingga database tidak pernah
// menunjuk ke nilai yang terbukti tidak bisa dipakai, misalnya folder yang
// tidak dapat ditulisi.
func (s *SettingsService) SetApplier(key string, fn Applier) {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	s.appliers[key] = fn
}

// Load membaca override dari penyimpanan dan menyiapkan nilai live.
func (s *SettingsService) Load(ctx context.Context) error {
	values, err := s.effective(ctx)
	if err != nil {
		return err
	}
	s.setLive(values)
	return nil
}

// Live mengembalikan nilai yang dibaca ulang setiap permintaan.
func (s *SettingsService) Live() LiveSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.live
}

// Effective mengembalikan nilai yang berlaku: bawaan ditimpa override yang
// tersimpan. Dipakai saat startup untuk menyusun komponen yang hanya
// membaca setelannya sekali.
func (s *SettingsService) Effective(ctx context.Context) (map[string]string, error) {
	return s.effective(ctx)
}

func (s *SettingsService) effective(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string, len(s.defaults))
	for k, v := range s.defaults {
		out[k] = v
	}

	stored, err := s.store.All(ctx)
	if err != nil {
		return nil, err
	}
	for k, v := range stored {
		if _, known := out[k]; known {
			out[k] = v
		}
	}
	return out, nil
}

func (s *SettingsService) setLive(values map[string]string) {
	live := LiveSettings{
		DefaultPresetID: values[KeyDefaultPreset],
		FilenameMode:    domain.FilenameMode(values[KeyFilenameMode]),
		MaxQueueDepth:   atoiOr(values[KeyMaxQueueDepth], 50),
		ToolUpdateCheck: values[KeyToolUpdateCheck] == "true",

		IdleShutdownMinutes: atoiOr(values[KeyIdleShutdown], 30),
	}

	s.mu.Lock()
	s.live = live
	s.mu.Unlock()
}

// List mengembalikan setelan yang dapat diubah dari UI beserta metadatanya.
func (s *SettingsService) List(ctx context.Context) ([]SettingView, error) {
	values, err := s.effective(ctx)
	if err != nil {
		return nil, err
	}

	presetIDs, err := s.presetIDs(ctx)
	if err != nil {
		return nil, err
	}

	order := []string{
		KeyOutputDir, KeyDefaultPreset, KeyFilenameMode, KeyMaxConcurrent,
		KeyMaxQueueDepth, KeyIdleShutdown, KeyToolUpdateCheck, KeyLogLevel,
	}

	views := make([]SettingView, 0, len(order))
	for _, key := range order {
		if notImplemented[key] {
			continue
		}
		v := SettingView{
			Key:             key,
			Value:           values[key],
			Kind:            kindOf(key),
			RequiresRestart: restartRequired[key],
		}
		switch key {
		case KeyDefaultPreset:
			v.Options = presetIDs
		case KeyFilenameMode:
			v.Options = []string{"title", "title-uploader", "uploader-title", "id"}
		case KeyLogLevel:
			v.Options = []string{"debug", "info", "warn", "error"}
		}
		views = append(views, v)
	}
	return views, nil
}

func (s *SettingsService) presetIDs(ctx context.Context) ([]string, error) {
	presets, err := s.presets.List(ctx, false)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(presets))
	for _, p := range presets {
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// Update memvalidasi, menerapkan, lalu menyimpan perubahan.
//
// Validasi terjadi untuk seluruh kunci sebelum satu pun diterapkan. Bila
// penerapan atau penyimpanan gagal di tengah jalan, kunci yang sudah
// terlanjur diterapkan dikembalikan ke nilai lamanya, sehingga komponen
// yang berjalan dan isi database tidak pernah berbeda.
func (s *SettingsService) Update(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return domain.NewError(domain.CodeInternal, domain.ClassLocal, "tidak ada perubahan")
	}

	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	presetIDs, err := s.presetIDs(ctx)
	if err != nil {
		return err
	}
	for key, value := range values {
		if err := validate(key, value, presetIDs); err != nil {
			return err
		}
	}

	current, err := s.effective(ctx)
	if err != nil {
		return err
	}

	// Urutan tetap supaya perilaku dan rollback dapat diulang persis.
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var applied []string
	for _, key := range keys {
		fn, ok := s.appliers[key]
		if !ok || values[key] == current[key] {
			continue
		}
		if err := fn(values[key]); err != nil {
			s.rollback(applied, current)
			return &domain.Error{
				Code:    domain.CodeInvalidSetting,
				Class:   domain.ClassLocal,
				Detail:  err.Error(),
				Details: map[string]string{"key": key},
				Cause:   err,
			}
		}
		applied = append(applied, key)
	}

	if err := s.store.Put(ctx, values); err != nil {
		s.rollback(applied, current)
		return err
	}

	merged, err := s.effective(ctx)
	if err != nil {
		return err
	}
	s.setLive(merged)
	return nil
}

// rollback memasang kembali nilai lama pada komponen yang sudah diubah.
//
// Error diabaikan: nilai lama itu sebelumnya sedang berlaku, jadi
// memasangnya kembali tidak punya jalan gagal yang dapat ditangani di sini.
func (s *SettingsService) rollback(keys []string, previous map[string]string) {
	for _, key := range keys {
		_ = s.appliers[key](previous[key])
	}
}

// validate memeriksa satu setelan.
func validate(key, value string, presetIDs []string) error {
	reject := func(detail string) error {
		return &domain.Error{
			Code: domain.CodeInvalidSetting, Class: domain.ClassLocal, Detail: detail,
			Details: map[string]string{"key": key},
		}
	}

	switch key {
	case KeyOutputDir:
		if value == "" {
			return reject("direktori keluaran tidak boleh kosong")
		}
		if !filepath.IsAbs(value) {
			return reject("direktori keluaran harus berupa path absolut")
		}

	case KeyMaxConcurrent:
		// Lebih dari dua atau tiga unduhan paralel dari satu IP memicu
		// throttling di sisi sumber, jadi batas atasnya sengaja rendah.
		return rangeCheck(key, value, 1, 8)

	case KeyMaxQueueDepth:
		return rangeCheck(key, value, 1, 500)

	case KeyIdleShutdown:
		// Nol berarti idle shutdown dimatikan.
		return rangeCheck(key, value, 0, 1440)

	case KeyDefaultPreset:
		if slices.Contains(presetIDs, value) {
			return nil
		}
		return reject(fmt.Sprintf("preset %q tidak dikenal", value))

	case KeyFilenameMode:
		if !domain.FilenameMode(value).Valid() {
			return reject(fmt.Sprintf("filename_mode %q tidak dikenal", value))
		}

	case KeyToolUpdateCheck:
		if value != "true" && value != "false" {
			return reject("tool_update_check harus true atau false")
		}

	case KeyLogLevel:
		switch value {
		case "debug", "info", "warn", "error":
		default:
			return reject(fmt.Sprintf("log_level %q tidak dikenal", value))
		}

	default:
		return reject(fmt.Sprintf("setelan %q tidak dikenal", key))
	}
	return nil
}

func rangeCheck(key, value string, min, max int) error {
	invalid := func(detail string) error {
		return &domain.Error{
			Code: domain.CodeInvalidSetting, Class: domain.ClassLocal, Detail: detail,
			Details: map[string]string{"key": key},
		}
	}

	n, err := strconv.Atoi(value)
	if err != nil {
		return invalid(fmt.Sprintf("%s harus berupa angka", key))
	}
	if n < min || n > max {
		return invalid(fmt.Sprintf("%s harus antara %d dan %d", key, min, max))
	}
	return nil
}

func kindOf(key string) string {
	switch key {
	case KeyOutputDir:
		return "path"
	case KeyMaxConcurrent, KeyMaxQueueDepth, KeyIdleShutdown:
		return "int"
	case KeyToolUpdateCheck:
		return "bool"
	case KeyDefaultPreset, KeyFilenameMode, KeyLogLevel:
		return "enum"
	default:
		return "string"
	}
}

func atoiOr(s string, fallback int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return fallback
}

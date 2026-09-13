package application

import (
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// Kebijakan auto-retry. Lihat planning §19 "Klasifikasi error dan kebijakan
// retry".
const (
	// MaxAutoRetries adalah jumlah pengulangan otomatis per job, di luar
	// percobaan pertama.
	MaxAutoRetries = 3

	// ThrottleCooldown adalah lama job paralel diturunkan ke satu setelah
	// sumber membalas HTTP 429.
	ThrottleCooldown = 5 * time.Minute
)

var (
	transientDelays = [MaxAutoRetries]time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second}
	throttledDelays = [MaxAutoRetries]time.Duration{30 * time.Second, 2 * time.Minute, 5 * time.Minute}
)

// RetryPlan menentukan apakah sebuah kegagalan diulang otomatis dan berapa
// lama jedanya.
//
// attempt adalah jumlah auto-retry yang sudah dijalankan untuk job ini.
// jitter mengembalikan bilangan acak [0, 1).
//
// Hanya kelas transient dan throttled yang diulang. Kegagalan permanen,
// lokal, dan tool usang tidak akan berubah hasilnya bila dicoba lagi; yang
// dibutuhkan adalah tindakan pengguna.
func RetryPlan(err *domain.Error, attempt int, jitter func() float64) (time.Duration, bool) {
	if err == nil || attempt < 0 || attempt >= MaxAutoRetries {
		return 0, false
	}

	switch err.Class {
	case domain.ClassTransient:
		return transientDelays[attempt], true
	case domain.ClassThrottled:
		// Jeda diacak ±20%: beberapa job yang terkena 429 bersamaan tidak
		// kembali serempak lalu memicu pembatasan berikutnya.
		factor := 0.8 + 0.4*jitter()
		return time.Duration(float64(throttledDelays[attempt]) * factor), true
	default:
		return 0, false
	}
}

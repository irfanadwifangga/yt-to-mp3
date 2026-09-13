package application_test

import (
	"testing"
	"time"

	"github.com/irfanadwifangga/yt-to-mp3/internal/application"
	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

func fixed(v float64) func() float64 { return func() float64 { return v } }

func TestRetryPlanTransientBerjenjang(t *testing.T) {
	err := domain.NewError(domain.CodeDownloadFailed, domain.ClassTransient, "reset")

	for attempt, want := range []time.Duration{2 * time.Second, 8 * time.Second, 30 * time.Second} {
		got, ok := application.RetryPlan(err, attempt, fixed(0.5))
		if !ok || got != want {
			t.Errorf("attempt %d: jeda = %v (ok %v), mau %v", attempt, got, ok, want)
		}
	}
	if _, ok := application.RetryPlan(err, application.MaxAutoRetries, fixed(0.5)); ok {
		t.Error("pengulangan melewati batas masih diizinkan")
	}
}

// Jeda throttled diacak supaya job yang terkena 429 bersamaan tidak kembali
// serempak.
func TestRetryPlanThrottledDiacakDalamBatas(t *testing.T) {
	err := domain.NewError(domain.CodeRateLimited, domain.ClassThrottled, "429")

	low, ok := application.RetryPlan(err, 0, fixed(0))
	if !ok || low != 24*time.Second {
		t.Errorf("jitter 0: %v, mau 24s", low)
	}
	high, _ := application.RetryPlan(err, 0, fixed(0.999999))
	if high < 35*time.Second || high > 36*time.Second {
		t.Errorf("jitter hampir 1: %v, mau sekitar 36s", high)
	}
}

func TestRetryPlanTidakMengulangKegagalanNonSementara(t *testing.T) {
	for _, class := range []domain.ErrorClass{
		domain.ClassPermanent, domain.ClassLocal, domain.ClassToolOutdated,
	} {
		err := domain.NewError(domain.CodeVideoPrivate, class, "x")
		if _, ok := application.RetryPlan(err, 0, fixed(0.5)); ok {
			t.Errorf("kelas %s diulang otomatis", class)
		}
	}
	if _, ok := application.RetryPlan(nil, 0, fixed(0.5)); ok {
		t.Error("error nil diulang otomatis")
	}
}

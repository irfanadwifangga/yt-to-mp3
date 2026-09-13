package domain_test

import (
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

func TestStatusTerminal(t *testing.T) {
	terminal := map[domain.JobStatus]bool{
		domain.StatusCompleted: true,
		domain.StatusFailed:    true,
		domain.StatusCancelled: true,
	}

	for _, s := range domain.AllStatuses() {
		if got, want := s.IsTerminal(), terminal[s]; got != want {
			t.Errorf("%s.IsTerminal() = %v, mau %v", s, got, want)
		}
		if got, want := s.IsActive(), !terminal[s]; got != want {
			t.Errorf("%s.IsActive() = %v, mau %v", s, got, want)
		}
		if !s.Valid() {
			t.Errorf("%s seharusnya valid", s)
		}
	}

	if domain.JobStatus("entah").Valid() {
		t.Error("status tak dikenal seharusnya tidak valid")
	}
	// Status tak dikenal jangan sampai terbaca terminal, karena itu akan
	// membuat job macet dianggap selesai.
	if domain.JobStatus("entah").IsTerminal() {
		t.Error("status tak dikenal seharusnya tidak terminal")
	}
}

// Sekali terminal, job tidak boleh hidup lagi. Tanpa aturan ini, retry bisa
// menimpa hasil yang sudah di-commit.
func TestTidakAdaTransisiKeluarDariTerminal(t *testing.T) {
	for _, from := range []domain.JobStatus{
		domain.StatusCompleted, domain.StatusFailed, domain.StatusCancelled,
	} {
		for _, to := range domain.AllStatuses() {
			if domain.CanTransition(from, to) {
				t.Errorf("transisi %s -> %s seharusnya ditolak", from, to)
			}
		}
	}
}

func TestCanTransition(t *testing.T) {
	tests := []struct {
		from domain.JobStatus
		to   domain.JobStatus
		want bool
	}{
		// Jalur normal.
		{domain.StatusQueued, domain.StatusResolving, true},
		{domain.StatusResolving, domain.StatusDownloading, true},
		{domain.StatusDownloading, domain.StatusConverting, true},
		{domain.StatusConverting, domain.StatusVerifying, true},
		{domain.StatusVerifying, domain.StatusCompleted, true},

		// Job yang masih antre belum punya proses OS, jadi boleh langsung
		// dibatalkan tanpa melewati cancelling.
		{domain.StatusQueued, domain.StatusCancelled, true},
		{domain.StatusQueued, domain.StatusCancelling, false},

		// Job yang sedang berjalan wajib lewat cancelling supaya process
		// tree sempat dimatikan sebelum status terminal ditulis.
		{domain.StatusDownloading, domain.StatusCancelled, false},
		{domain.StatusDownloading, domain.StatusCancelling, true},
		{domain.StatusCancelling, domain.StatusCancelled, true},

		// Tidak boleh melompati fase.
		{domain.StatusQueued, domain.StatusDownloading, false},
		{domain.StatusResolving, domain.StatusConverting, false},
		{domain.StatusDownloading, domain.StatusCompleted, false},

		// Tidak boleh mundur ke fase sebelumnya.
		{domain.StatusConverting, domain.StatusDownloading, false},
		{domain.StatusVerifying, domain.StatusResolving, false},

		// Auto-retry mengembalikan job yang sedang berjalan ke antrean.
		// Job yang sedang dibatalkan tidak boleh hidup lagi lewat jalur ini.
		{domain.StatusDownloading, domain.StatusQueued, true},
		{domain.StatusVerifying, domain.StatusQueued, true},
		{domain.StatusCancelling, domain.StatusQueued, false},
		{domain.StatusQueued, domain.StatusQueued, false},

		// Status ke dirinya sendiri bukan transisi.
		{domain.StatusDownloading, domain.StatusDownloading, false},

		// Status tak dikenal tidak punya transisi apa pun.
		{domain.JobStatus("entah"), domain.StatusQueued, false},
	}

	for _, tc := range tests {
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			if got := domain.CanTransition(tc.from, tc.to); got != tc.want {
				t.Errorf("CanTransition(%s, %s) = %v, mau %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

// Setiap state aktif harus punya jalan menuju failed, kalau tidak kegagalan
// di fase itu akan menggantungkan job selamanya.
func TestSetiapStateAktifBisaGagal(t *testing.T) {
	for _, s := range domain.AllStatuses() {
		if s.IsTerminal() {
			continue
		}
		if !domain.CanTransition(s, domain.StatusFailed) {
			t.Errorf("%s tidak punya jalur ke failed", s)
		}
	}
}

func TestFilenameMode(t *testing.T) {
	valid := []domain.FilenameMode{
		domain.FilenameTitle, domain.FilenameTitleUploader,
		domain.FilenameUploaderTitle, domain.FilenameID,
	}
	for _, m := range valid {
		if !m.Valid() {
			t.Errorf("%s seharusnya valid", m)
		}
	}
	for _, m := range []domain.FilenameMode{"", "custom", "TITLE"} {
		if m.Valid() {
			t.Errorf("%q seharusnya tidak valid", m)
		}
	}
}

// Event progress tidak boleh dipersist; kalau lolos ke sini, ribuan baris
// per job akan masuk database. Lihat ADR-023.
func TestEventTypeTidakMemuatProgress(t *testing.T) {
	if domain.EventType("progress").Valid() {
		t.Error("progress seharusnya tidak boleh dipersist")
	}
	for _, tp := range []domain.EventType{
		domain.EventState, domain.EventError, domain.EventDone,
	} {
		if !tp.Valid() {
			t.Errorf("%s seharusnya valid", tp)
		}
	}
}

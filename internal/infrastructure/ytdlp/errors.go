package ytdlp

import (
	"fmt"
	"strings"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// pattern memetakan potongan pesan stderr yt-dlp ke kode domain.
//
// Urutan penting: pola yang lebih spesifik harus diperiksa lebih dulu.
// Pesan mentah tidak pernah diteruskan ke UI, hanya masuk log dan
// job_events. Lihat docs planning "Kode error dan lokalisasi".
type pattern struct {
	needle string
	code   domain.ErrorCode
	class  domain.ErrorClass
}

var patterns = []pattern{
	{"private video", domain.CodeVideoPrivate, domain.ClassPermanent},
	{"sign in to confirm your age", domain.CodeAgeRestricted, domain.ClassPermanent},
	{"age-restricted", domain.CodeAgeRestricted, domain.ClassPermanent},
	{"inappropriate for some users", domain.CodeAgeRestricted, domain.ClassPermanent},
	// Menangkap dua bentuk yang dipakai yt-dlp: "has not made this video
	// available in your country" dan "is not available in your country".
	{"available in your country", domain.CodeGeoBlocked, domain.ClassPermanent},
	{"blocked it in your country", domain.CodeGeoBlocked, domain.ClassPermanent},
	{"who has blocked it on copyright grounds", domain.CodeGeoBlocked, domain.ClassPermanent},
	{"members-only", domain.CodeVideoPrivate, domain.ClassPermanent},
	{"video has been removed", domain.CodeVideoUnavailable, domain.ClassPermanent},
	{"video unavailable", domain.CodeVideoUnavailable, domain.ClassPermanent},
	{"this video is not available", domain.CodeVideoUnavailable, domain.ClassPermanent},
	// Deteksi livestream yang sesungguhnya memakai field is_live pada JSON;
	// pola di bawah hanya jaring pengaman. Sengaja spesifik: substring
	// pendek seperti "is live" ikut cocok di tengah kata lain.
	{"live event will begin", domain.CodeLiveNotSupported, domain.ClassPermanent},
	{"this live stream", domain.CodeLiveNotSupported, domain.ClassPermanent},

	// yt-dlp tidak menemukan FFmpeg. Mengulang tidak akan menolong, dan
	// sebelum pola ini ada, setiap job diulang tiga kali dengan pesan
	// "unduhan gagal" yang menyesatkan.
	{"ffmpeg not found", domain.CodeToolMissing, domain.ClassLocal},

	{"http error 429", domain.CodeRateLimited, domain.ClassThrottled},
	{"too many requests", domain.CodeRateLimited, domain.ClassThrottled},

	// Kelas ini menandakan yt-dlp tertinggal dari perubahan sisi YouTube.
	// Mengulanginya tidak akan menolong; yang dibutuhkan adalah update tool.
	{"nsig extraction failed", domain.CodeToolOutdated, domain.ClassToolOutdated},
	{"signature extraction failed", domain.CodeToolOutdated, domain.ClassToolOutdated},
	{"unable to extract", domain.CodeToolOutdated, domain.ClassToolOutdated},
	{"please report this issue", domain.CodeToolOutdated, domain.ClassToolOutdated},

	{"unable to download webpage", domain.CodeDownloadFailed, domain.ClassTransient},
	{"connection reset", domain.CodeDownloadFailed, domain.ClassTransient},
	{"temporary failure in name resolution", domain.CodeDownloadFailed, domain.ClassTransient},
	{"timed out", domain.CodeDownloadFailed, domain.ClassTransient},
}

// mapStderr menerjemahkan stderr yt-dlp jadi error domain.
func mapStderr(stderr []byte, exitCode int) *domain.Error {
	text := strings.ToLower(string(stderr))

	for _, p := range patterns {
		if strings.Contains(text, p.needle) {
			return domain.NewError(p.code, p.class, truncate(string(stderr)))
		}
	}

	// Tidak dikenali: diperlakukan sebagai transient supaya kegagalan
	// sesaat tetap bisa pulih, tapi tetap dibatasi jumlah percobaan.
	return domain.NewError(domain.CodeDownloadFailed, domain.ClassTransient,
		fmt.Sprintf("yt-dlp keluar dengan kode %d: %s", exitCode, truncate(string(stderr))))
}

// truncate memangkas detail agar log tidak dibanjiri keluaran panjang.
func truncate(s string) string {
	const limit = 500
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

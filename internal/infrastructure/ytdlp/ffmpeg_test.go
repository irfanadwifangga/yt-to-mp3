package ytdlp

import (
	"testing"

	"github.com/irfanadwifangga/yt-to-mp3/internal/domain"
)

// yt-dlp harus memakai FFmpeg yang sama dengan yang ditemukan aplikasi,
// bukan mencarinya sendiri lewat PATH.
func TestDownloadArgsMenunjukFFmpeg(t *testing.T) {
	const ffmpeg = `C:\Users\Nama Pengguna\tools\ffmpeg.exe`
	args := downloadArgs("https://www.youtube.com/watch?v=dQw4w9WgXcQ", "/tmp/job", ffmpeg)

	i := indexOf(args, "--ffmpeg-location")
	if i == -1 || i+1 >= len(args) || args[i+1] != ffmpeg {
		t.Fatalf("--ffmpeg-location tidak menunjuk %q: %v", ffmpeg, args)
	}
	if sep := indexOf(args, "--"); i > sep {
		t.Error("--ffmpeg-location berada setelah separator --")
	}
}

// Kegagalan menemukan FFmpeg bukan gangguan jaringan; mengulangnya hanya
// membuang waktu pengguna.
func TestMapStderrFFmpegTidakDitemukan(t *testing.T) {
	err := mapStderr([]byte("ERROR: Preprocessing: ffmpeg not found. "+
		"Please install or provide the path using --ffmpeg-location"), 1)

	if err.Code != domain.CodeToolMissing {
		t.Errorf("kode = %s, mau %s", err.Code, domain.CodeToolMissing)
	}
	if err.Retryable() {
		t.Error("kegagalan menemukan FFmpeg tidak boleh diulang otomatis")
	}
}

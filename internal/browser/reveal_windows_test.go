//go:build windows

package browser

import "testing"

// Explorer yang menerima "/select,..." dalam tanda kutip utuh diam-diam
// membuka Documents alih-alih folder berkasnya. Path yang memuat spasi,
// seperti nama pengguna Windows dua kata, adalah kasus yang paling umum.
func TestExplorerSelectCommandLine(t *testing.T) {
	tests := map[string]struct{ path, want string }{
		"nama pengguna berspasi": {
			`C:\Users\Nama Lengkap\Music\yt-to-mp3\Lagu (Official Video).mp3`,
			`explorer.exe /select,"C:\Users\Nama Lengkap\Music\yt-to-mp3\Lagu (Official Video).mp3"`,
		},
		"garis miring biasa dari setelan": {
			`C:/Users/Nama Lengkap/Music/lagu.mp3`,
			`explorer.exe /select,"C:\Users\Nama Lengkap\Music\lagu.mp3"`,
		},
		"karakter non-ASCII": {
			`D:\Musik\back number - 水平線.mp3`,
			`explorer.exe /select,"D:\Musik\back number - 水平線.mp3"`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := explorerSelectCommandLine(tc.path); got != tc.want {
				t.Errorf("baris perintah\n dapat %s\n mau   %s", got, tc.want)
			}
		})
	}
}

package dialog

import "github.com/ncruces/zenity"

// Error menampilkan pesan kesalahan native dan menunggu sampai ditutup.
//
// Kegagalan menampilkan dialog diabaikan: pemanggilnya selalu menulis pesan
// yang sama ke stderr lebih dulu.
func Error(title, text string) {
	_ = zenity.Error(text, zenity.Title(title))
}

// Info menampilkan pesan informasi native dan menunggu sampai ditutup.
func Info(title, text string) {
	_ = zenity.Info(text, zenity.Title(title))
}

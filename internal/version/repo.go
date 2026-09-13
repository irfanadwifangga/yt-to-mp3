package version

// Repo adalah repositori GitHub tempat rilis aplikasi diterbitkan.
//
// Cek pembaruan aplikasi membaca rilis terbaru dari sini. Selama
// repositorinya private, API GitHub menjawab 404 tanpa autentikasi dan cek
// itu tidak menghasilkan apa pun; cek tool tetap berjalan.
const Repo = "irfanadwifangga/yt-to-mp3"

// ReleasesURL adalah halaman unduhan rilis terbaru. Dibentuk dari konstanta,
// tidak pernah dari jawaban API, sehingga UI tidak bisa diarahkan ke tautan
// lain.
const ReleasesURL = "https://github.com/" + Repo + "/releases/latest"

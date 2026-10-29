# YouTube to MP3 & MP4

[English](README.en.md) · **Bahasa Indonesia**

Konverter video YouTube menjadi audio MP3 atau video MP4 berbentuk aplikasi desktop lokal (nama teknis: `yt-to-mp3`, dipakai untuk repositori, exe, dan direktori data): satu berkas program Go yang menjalankan server di loopback dan menyajikan SPA React yang tersemat di dalamnya. Tidak ada layanan komputasi awan; semua pemrosesan dan data tetap di komputermu.

> **Status: fungsi inti dan penguatan keamanan selesai.** Analisis, antrean, konversi, riwayat, setelan, pemasangan perkakas sekali klik, pencatatan log, perawatan data, dan penghentian otomatis saat tidak aktif sudah berjalan, dan target NFR terukur lolos. Versi terbaru adalah [v0.3.0](https://github.com/irfanadwifangga/yt-to-mp3/releases/latest), dan berkas program belum ditandatangani. Baca [Keterbatasan yang diketahui](#keterbatasan-yang-diketahui) sebelum memakai.

## Fitur

- **Analisis tanpa mengunduh** — tempel tautan dan pratinjau judul, kanal, durasi, serta codec sumber langsung muncul.
- **Progres langsung** per tugas konversi lewat SSE, ditampilkan sebagai sampul video yang terisi warna dari bawah ke atas. Membatalkan tugas konversi menghentikan seluruh pohon proses, termasuk FFmpeg yang dijalankan yt-dlp, lalu membersihkan berkas sementara.
- **Daftar selesai** — buka folder hasil di pengelola berkas, simpan salinan, coba lagi yang gagal, atau hapus dari daftar (dengan atau tanpa berkasnya). Riwayat panjang dimuat bertahap.
- **Percobaan ulang otomatis** — kegagalan jaringan sementara diulang otomatis hingga 3 kali dengan jeda bertambah. Bila YouTube membatasi permintaan (HTTP 429), jedanya lebih panjang dan tugas paralel diturunkan ke satu selama beberapa menit.
- **Audio atau video, dalam banyak format** — pilih **Audio** atau **Video**, lalu format dan kualitasnya:
  - Audio: MP3, M4A (AAC atau ALAC lossless), Opus (aliran media asli YouTube, disalin tanpa pengodean ulang), Ogg Vorbis, FLAC, dan WAV.
  - Video: MP4, MKV (aliran media asli YouTube tanpa pengodean ulang, cepat sampai 4K), MOV, WebM, AVI (Xvid + MP3 untuk pemutar lama), dan FLV.
  - Setiap format disertai satu kalimat penjelasan kegunaannya.
- **Lima preset MP3**: 128, 192, 256, 320 kbps CBR, dan VBR V0. Keluaran 48 kHz stereo dengan tag ID3v2.3 dan sampul persegi tersemat.
- **Video dalam 360p, 480p, 720p, 1080p, atau resolusi terbaik yang tersedia**. MP4 selalu video H.264 dengan audio AAC supaya berkasnya bisa diputar di perangkat apa pun, termasuk pemutar bawaan Windows, TV, dan ponsel lama. Sampai 1080p, aliran media H.264 dari YouTube disalin apa adanya (cepat, tanpa penurunan kualitas). Sumber tanpa H.264 di resolusi yang dipilih, termasuk semua resolusi di atas 1080p, dikodekan ulang; prosesnya jauh lebih lama dan aplikasi memperingatkannya lebih dulu. Pilihan kualitas di atas resolusi sumber diberi tanda.
- **Judul dan artis bisa disunting sebelum konversi**, terisi saran yang sudah dirapikan (tanpa `(Official Video)` dan sejenisnya). Keduanya dipakai untuk tag dan nama berkas. Untuk video yang terhubung ke YouTube Music, saran diambil dari katalog, dan tag album serta tahun rilis ikut ditulis.
- **Peringatan konversi ganda** bila video yang sama pernah dikonversi dengan format dan kualitas yang sama.
- **Setelan dalam dialog** (tombol **Setelan** atau `Ctrl+,`), dengan folder keluaran dipilih lewat dialog folder bawaan sistem operasi.
- **Bahasa Indonesia dan Inggris**, serta tema terang, gelap, atau mengikuti sistem.
- **Jendela sendiri di Windows** — UI dibuka sebagai jendela aplikasi Microsoft Edge tanpa tab maupun bilah alamat. Sistem lain memakai peramban bawaan.
- **Satu program, satu proses aplikasi** — menjalankannya lagi membuka aplikasi yang sudah berjalan, bukan server kedua.

## Instalasi

**Windows:** unduh `yt-to-mp3_<versi>_windows_amd64_setup.exe` dari halaman Releases lalu jalankan. Tidak perlu hak admin. Pemasang membuat pintasan di Start Menu, dan aplikasi bisa dihapus dari **Settings → Apps**. Menghapus aplikasi tidak menghapus riwayat maupun berkas hasil konversi.

Pemasang dan berkas exe belum ditandatangani, sehingga SmartScreen dapat menampilkan "Windows protected your PC". Pilih **More info** lalu **Run anyway**. Cocokkan berkas dengan `.sha256` di halaman rilis bila ingin memastikan unduhannya utuh.

Aplikasi terbuka di jendelanya sendiri. Menutup jendela menghentikan aplikasi begitu tidak ada konversi yang berjalan; konversi yang masih berlangsung diselesaikan lebih dulu. Tombol **Keluar** di kanan atas menghentikannya seketika. Saat pertama dibuka, pasang yt-dlp dan FFmpeg dari **Setelan → Tool**. Bila Microsoft Edge tidak tersedia, UI dibuka di peramban bawaan.

**macOS dan Linux:** unduh arsip `tar.gz` untuk arsitektur yang sesuai, ekstrak, lalu jalankan `yt-to-mp3`. Di macOS, jalankan `xattr -d com.apple.quarantine ./yt-to-mp3` lebih dulu karena berkas program belum ditandatangani. UI dibuka di peramban bawaan.

## Kebutuhan

| Perkakas | Versi | Dipakai untuk |
| --- | --- | --- |
| Go | 1.27+ | Membangun backend |
| Node.js | 24+ | Membangun SPA; tidak dibutuhkan saat aplikasi berjalan |
| yt-dlp | terbaru | Membaca metadata dan mengunduh audio atau video |
| FFmpeg dan ffprobe | dengan `libmp3lame` dan `libx264`; diuji pada 9.0.1 | Konversi dan verifikasi hasil |

Pembangunan pertama mengunduh modul Go dan paket npm, jadi butuh jaringan sekali. Seluruh dependensi Go murni Go (tanpa cgo), sehingga kompilasi lintas platform tidak memerlukan perangkat kompilasi C.

### Memasang yt-dlp dan FFmpeg

Cara termudah: buka **Setelan → Tool** lalu tekan **Pasang**. Aplikasi mengunduh versi yang ditetapkan dalam manifes, memverifikasi SHA-256 sebelum mengekstrak, lalu memasangnya ke direktori data aplikasi. Sumbernya yt-dlp dari rilis resminya, FFmpeg dari GyanD (Windows) dan martin-riedl.de (Linux, macOS); alasannya di [ADR-031](yt-to-mp3-go-planning.md#4-keputusan-teknis).

Perkakas yang sudah dipasang lewat pengelola paket juga dipakai; aplikasi menemukannya dari `PATH`.

Windows:

```bash
winget install yt-dlp.yt-dlp
```

```bash
winget install Gyan.FFmpeg
```

macOS:

```bash
brew install yt-dlp ffmpeg
```

Linux: pasang FFmpeg dari paket distro. Untuk yt-dlp, pakai berkas program dari [rilis resminya](https://github.com/yt-dlp/yt-dlp/releases) — paket distro sering tertinggal, padahal yt-dlp harus terus mengikuti perubahan di sisi YouTube.

Perkakas yang dipasang ke direktori yang sudah ada di `PATH` terdeteksi dalam 30 detik tanpa membuka ulang aplikasi. Bila pemasangannya menambah direktori baru ke `PATH` — biasanya pada pemasangan pertama lewat winget — buka ulang aplikasi, karena proses yang sedang berjalan tidak melihat perubahan `PATH`.

## Mulai cepat

```bash
make install-web && make build && make run
```

Tanpa `make`:

```bash
npm --prefix web install && npm --prefix web run build && go build -o bin/yt-to-mp3 ./cmd/app && ./bin/yt-to-mp3
```

Aplikasi memilih port acak di `127.0.0.1` lalu membuka UI. Alamat pembukanya membawa token sesi sekali pakai; token itu langsung dipindahkan ke `sessionStorage` dan dihapus dari bilah alamat.

Cara memakai:

1. Tempel atau ketik URL video YouTube. Analisis berjalan otomatis begitu tautannya lengkap; tidak ada tombol yang perlu ditekan (Enter tetap bisa dipakai).
2. Pilih preset, lalu **Konversi**.
3. Pantau progres di **Sedang diproses**. Begitu selesai, notifikasi memberi tahu bahwa berkas sudah tersimpan di folder hasil (tertera di bagian bawah halaman), lengkap dengan tombol **Tampilkan di folder**. Berkas tidak perlu diunduh lagi; **Simpan salinan** di daftar Selesai hanya membuat duplikat lewat peramban.

Menghentikan aplikasi: tombol **Keluar** di kanan atas, menutup jendela aplikasi, atau `Ctrl+C` di terminal. Bila UI terbuka di tab browser, aplikasi juga berhenti sendiri setelah tab ditutup dan tidak ada tugas konversi selama 30 menit; batasnya bisa diubah atau dimatikan (0) di **Setelan → Lanjutan**. Tidak ada ikon baki sistem karena pustaka tray membutuhkan cgo, dan itu akan merusak target cross-compile ([ADR-021](yt-to-mp3-go-planning.md#4-keputusan-teknis)).

## Konfigurasi

Setelan dapat diubah dari dialog **Setelan** di UI, atau lewat `config.json` di direktori data (dibuat otomatis saat pertama kali dijalankan), atau lewat variabel lingkungan:

| Variabel | Setara dengan |
| --- | --- |
| `YT2MP3_OUTPUT_DIR` | `output_dir` |
| `YT2MP3_LOG_LEVEL` | `log_level` |
| `YT2MP3_MAX_CONCURRENT_JOBS` | `max_concurrent_jobs` |

Urutan prioritasnya: nilai bawaan, lalu `config.json`, lalu variabel lingkungan, lalu perubahan dari UI.

Hampir semua setelan berlaku seketika setelah disimpan dari UI, termasuk folder keluaran dan tingkat log. Pengecualiannya **jumlah tugas paralel**, yang baru berlaku setelah aplikasi dibuka ulang karena kapasitas pekerja disusun saat aplikasi dimulai.

Folder keluaran dipilih lewat dialog folder bawaan sistem operasi. Dialog itu dibuka oleh server lokal, bukan peramban, karena peramban tidak pernah memberi tahu halaman web jalur absolut sebuah folder. Di Linux dialognya membutuhkan `zenity`, `kdialog`, atau `qarma`; tanpa salah satunya, ketik jalur folder secara manual. Folder baru diuji bisa ditulisi sebelum disimpan, dan konversi yang sedang berjalan tetap diselesaikan di folder sebelumnya.

## Pengembangan

Backend dan frontend dijalankan terpisah supaya hot reload Vite tetap hidup. Dua terminal:

```bash
make dev-api
```

```bash
make dev-web
```

Backend memakai port tetap 8799 di mode dev (produksi tetap acak) karena proxy Vite butuh target yang bisa ditebak. Flag `-dev` mengizinkan origin `localhost:5173` dan tidak pernah aktif pada build rilis.

Token sesi hanya bisa masuk lewat URL, jadi buka SPA dengan menempelkan token dari output terminal `make dev-api` (baris `buka:`). URL bertoken itu sengaja hanya dicetak ke terminal dan tidak pernah ke berkas log:

```
http://localhost:5173/?token=<token-dari-log>
```

### Pemeriksaan

| Perintah | Isi |
| --- | --- |
| `make check` | Format, `go vet`, dan seluruh pengujian Go |
| `make typecheck` | Pemeriksaan tipe frontend |
| `make test-web` | Pengujian komponen frontend dengan vitest dan jsdom: analisis otomatis, hasil analisis basi, suntingan tag, peringatan konversi ganda, pembaruan yt-dlp, notifikasi, dan penggabungan riwayat |
| `make check-i18n` | Setiap kode kesalahan Go punya terjemahan `id` dan `en`, dan kunci kedua bahasa setara |
| `make test-integration` | Konversi dengan FFmpeg dan ffprobe sungguhan memakai fixture sintetis (nada sinus, sampul polos), ditambah pengujian menyeluruh alur tugas konversi dengan yt-dlp palsu: konversi, percobaan ulang otomatis, dan pembatalan saat mengunduh. Gagal bila ffmpeg tidak ditemukan |
| `make nfr` | Ukur target [NFR](yt-to-mp3-go-planning.md#22-non-functional-requirements) pada direktori data sementara; `URL="<tautan>"` ikut mengukur konversi dua tugas paralel |

Bentuk respons API dikunci berkas golden di `internal/api/testdata/golden/` dan ikut diperiksa `make test`. Perubahan kontrak yang disengaja ditulis ulang dengan `go test ./internal/api/ -run Kontrak -update`, lalu diff-nya ditinjau sebelum commit.

CI menjalankan seluruh pemeriksaan di atas kecuali `make nfr`, ditambah pengujian Go di Windows dan macOS (terminasi process tree dan rename atomik berbeda per OS). Workflow **Tool regression** memasang yt-dlp dan FFmpeg persis dari manifest di kelima target rilis lalu mengonversi fixture dengan binary hasil pasang; workflow itu jalan setiap kali kode tool atau manifest berubah, dan mingguan untuk menangkap URL hulu yang mati. `make help` menampilkan seluruh target.

Sebagian pengujian menjalankan proses sungguhan — pengujian penghentian pohon proses menjalankan proses anak dan cucu untuk membuktikan keduanya mati — sehingga paket `process` butuh beberapa detik.

### Rilis

Rilis dibangun GoReleaser dari tag `v*` lewat workflow **Release**: arsip untuk windows/amd64, linux/amd64, linux/arm64, darwin/amd64, dan darwin/arm64 dengan nama `yt-to-mp3_<versi>_<os>_<arch>`, beserta `checksums.txt`. Rilisnya dibuat sebagai **draft** dan baru terlihat publik setelah diterbitkan manual dari halaman Releases. Riwayat perubahan tiap versi ada di halaman [Releases](https://github.com/irfanadwifangga/yt-to-mp3/releases).

Sebelum memberi tag baru, pipeline-nya bisa divalidasi dengan menjalankan workflow **Release** secara manual (mode snapshot, tidak membuat rilis). Artefaknya dinamai sesuai berkas di dalamnya, misalnya `yt-to-mp3_<versi>_windows_amd64_setup`. Snapshot juga bisa dijalankan lokal bila goreleaser v2 terpasang:

```bash
make release-snapshot
```

Perkakas yt-dlp dan FFmpeg tidak pernah ikut di dalam arsip rilis; pengguna memasangnya dari aplikasi ([ADR-031](yt-to-mp3-go-planning.md#4-keputusan-teknis)). Penandatanganan digital belum tersedia: macOS butuh codesign dan notarization dengan akun Apple Developer, Windows butuh sertifikat code signing.

Build Windows berbeda dari target lain ([planning §25](yt-to-mp3-go-planning.md#25-build-dev-workflow-dan-rilis)):
- Di-link sebagai aplikasi GUI tanpa jendela konsol. Kesalahan saat aplikasi dimulai muncul sebagai dialog.
- Membawa ikon, info versi, dan manifest.
- Job **Windows installer** membungkusnya menjadi `setup.exe` dengan Inno Setup.

Untuk build lokal dengan ikon dan info versi, jalankan `make winres` sebelum `go build`. Ikon digambar ulang dari tanda merek dengan `make icon`.

## Gambaran arsitektur

![Gambaran arsitektur YouTube to MP3 & MP4: backend, UI tersemat, state lokal dan platform, serta tool konversi](diagram-ytmp3.png)

Diagram ini menunjukkan bagaimana komponen saling terhubung saat aplikasi berjalan:

- **Backend** — `cmd/app/main.go` merakit semuanya: penjaga single instance, API HTTP lokal beserta aliran progres tugas konversi lewat SSE, scheduler job, dan pipeline konversi. `internal/adapters` hanya menjembatani tipe infrastructure ke port application.
- **UI tersemat** — aplikasi React disematkan ke binary lewat `go:embed` dan disajikan server lokal yang sama. UI memanggil API lewat `api.ts` dan mengikuti progres tugas konversi lewat SSE dengan `useJobStream.ts`.
- **State lokal dan platform** — SQLite menyimpan job, riwayat, dan setelan, dengan skema yang dikembangkan lewat migrasi tersemat. Store berkas menulis MP3 dan MP4 hasil lewat commit atomik. Jendela aplikasi (`internal/browser`), dialog native (`internal/dialog`), dan "Tampilkan di folder" berinteraksi dengan sistem operasi.
- **Tool konversi** — pipeline menjalankan yt-dlp untuk mengunduh audio atau video dan FFmpeg untuk memeriksa serta mengonversi, keduanya sebagai proses anak lokal. Salinan terkelola kedua perkakas dipasang dan diperbarui dari dalam aplikasi.

Setelan, perawatan data, penghentian otomatis saat tidak aktif, dan pemeriksaan pembaruan tidak digambar supaya diagram tetap mudah dibaca. Semua komponen dibahas lengkap di [architecture.md](architecture.md).

## Struktur

```text
cmd/app/              entry point dan wiring dependensi
internal/
  adapters/           jembatan infrastructure ke port application, dipakai bersama test E2E
  api/                HTTP handler, middleware keamanan, SSE hub
  application/        use case, port, orkestrasi pipeline
  domain/             entity, state machine, taksonomi error
  infrastructure/
    db/               SQLite, repository, runner migrasi
    ffmpeg/           transcode, parsing progress, verifikasi dengan ffprobe
    fs/               reservasi nama, commit atomik, sanitasi nama, ruang disk
    process/          proses anak dan terminasi process tree per OS
    tools/            discovery dan instalasi yt-dlp serta FFmpeg
    ytdlp/            metadata, unduhan, pemetaan error
  worker/             scheduler dan worker pool
  config/             konfigurasi dan lokasi penyimpanan per OS
  instance/           penjagaan single instance
  browser/            peramban bawaan, jendela aplikasi Edge, dan pengelola berkas
  dialog/             dialog **native**
  version/            identitas build
web/                  SPA React + TypeScript, di-embed lewat go:embed
  src/locales/        kamus terjemahan id dan en
  scripts/            pemeriksa kelengkapan terjemahan
migrations/           migrasi SQL, di-embed
packaging/windows/    ikon, manifest, info versi, skrip Inno Setup
scripts/              pemutakhiran manifes perkakas dan pembuat ikon
docs/                 perencanaan, arsitektur, model data
```

Arah impor satu arah: `api` dan `infrastructure` bergantung pada `application`, `application` hanya pada `domain`, dan `domain` tidak bergantung pada apa pun. Detailnya di [architecture.md](architecture.md).

## API

Seluruh endpoint berada di bawah `/api` dan mewajibkan header `X-Session-Token`, kecuali `GET /api/ping` yang dipakai untuk mendeteksi instance yang sudah berjalan. Kontrak lengkapnya di [planning §7](yt-to-mp3-go-planning.md#7-kontrak-api).

| Endpoint | Fungsi |
| --- | --- |
| `GET /health` | Status aplikasi, tool, dan antrean |
| `GET /tools` · `POST /tools/install` | Status dan instalasi tool |
| `POST /tools/check` · `POST /tools/update` | Periksa versi terbaru; perbarui yt-dlp atau FFmpeg (FFmpeg hanya di Windows) |
| `GET /presets` | Daftar preset |
| `POST /metadata` | Analisis URL tanpa mengunduh |
| `POST /jobs` · `GET /jobs` · `GET /jobs/{id}` | Buat, daftar, dan baca job |
| `GET /jobs/{id}/events` | Progres langsung (SSE, mendukung `Last-Event-ID`) |
| `POST /jobs/{id}/cancel` · `POST /jobs/{id}/retry` · `DELETE /jobs/{id}` | Batalkan, ulangi, hapus (`?delete_file=true` ikut menghapus berkas hasil) |
| `GET /files/{id}` · `POST /files/{id}/reveal` | Unduh hasil, buka lokasinya |
| `GET /settings` · `PUT /settings` | Baca dan ubah setelan |
| `POST /dialogs/folder` | Buka dialog pemilih folder bawaan sistem |
| `POST /shutdown` | Hentikan aplikasi |

## Lokasi data

| OS | Data | Keluaran bawaan |
| --- | --- | --- |
| Windows | `%LOCALAPPDATA%\yt-to-mp3\` | `%USERPROFILE%\Music\yt-to-mp3\` |
| macOS | `~/Library/Application Support/yt-to-mp3/` | `~/Music/yt-to-mp3/` |
| Linux | `$XDG_DATA_HOME/yt-to-mp3/` | `$XDG_MUSIC_DIR` atau `~/Music/yt-to-mp3/` |

Direktori data berisi database SQLite (`db/app.db`), `config.json`, tool yang dipasang aplikasi, berkas sementara, log, profil Edge untuk jendela aplikasi di Windows (`window/`), dan `runtime.json` selama aplikasi berjalan.

Log ditulis ke `logs/app.log` sekaligus ke terminal, dirotasi harian menjadi `logs/app-YYYY-MM-DD.log`, dan arsip yang lebih tua dari 7 hari dibuang. Sejak v0.3.0, log juga bisa dilihat langsung dari dalam aplikasi lewat penampil log dengan pembaruan langsung, pencarian, penyaringan tingkat log, dan tombol salin. Lampirkan log saat melaporkan bug; token sesi tidak pernah tertulis di sana.

Aplikasi merawat datanya sendiri saat dibuka lalu setiap jam: jejak event job yang selesai lebih dari 30 hari dipangkas (job dan berkasnya tetap ada), cache metadata kedaluwarsa dibuang, berkas sementara yatim dibersihkan, dan status berkas hasil disamakan dengan disk. Berkas yang dihapus di luar aplikasi ditandai hilang, dan tersedia lagi jika muncul kembali, misalnya setelah penyimpanan eksternal disambungkan kembali.

## Keamanan

Server lokal bukan berarti server privat: situs web mana pun yang sedang dibuka pengguna dapat mengirim request ke `127.0.0.1`. Karena itu server ini menerapkan bind loopback eksplisit, allowlist header `Host` sebagai penangkal DNS rebinding, pemeriksaan `Origin` pada setiap request yang mengubah state, token sesi acak per proses, CORS deny-all, dan batas ukuran body. Path berkas tidak pernah diterima dari klien, dan argumen tool tidak pernah melewati shell. Perinciannya di [planning §15](yt-to-mp3-go-planning.md#15-model-keamanan-lokal).

## Keterbatasan yang diketahui

- **Pembaruan FFmpeg satu klik hanya di Windows.** yt-dlp bisa diperbarui dari **Setelan → Tool** di semua sistem (diverifikasi dengan `SHA2-256SUMS` rilis resminya). Di Windows, FFmpeg juga bisa (diverifikasi dengan digest SHA-256 yang diterbitkan GitHub untuk berkas rilisnya); salinan barunya dipasang di folder tool milik aplikasi, dan FFmpeg yang dipasang lewat winget dibiarkan apa adanya. Di Linux dan macOS, FFmpeg tetap mengikuti manifes dengan versi yang telah ditetapkan; untuk memperbaruinya, jalankan `make update-tools` lalu commit.
- **Berkas program dan pemasang belum ditandatangani**, sehingga SmartScreen di Windows dan Gatekeeper di macOS memperingatkan saat pertama dijalankan.
- **Aplikasi hanya memberi tahu bila ada versi baru**, tidak memperbarui dirinya sendiri; unduh installer baru dari halaman rilis. Cek ini membaca rilis GitHub `irfanadwifangga/yt-to-mp3`.
- **Jendela aplikasi di Windows adalah Microsoft Edge dengan profil terpisah**, bukan jendela bawaan sistem. Bila Edge tidak ada, antarmuka beralih ke peramban bawaan, dan menutup tab itu tidak langsung menghentikan aplikasi; penghentian otomatis saat tidak aktif yang mengakhirinya kemudian.

Sengaja tidak didukung: playlist, siaran langsung, video yang memerlukan akun masuk atau dibatasi usia, dan pilihan codec atau parameter encoder bebas (setiap format punya satu codec tujuan yang tetap). Daftar lengkapnya di [non-goals](yt-to-mp3-go-planning.md#3-non-goals).

## Dokumentasi

| Dokumen | Isi |
| --- | --- |
| [yt-to-mp3-go-planning.md](yt-to-mp3-go-planning.md) | Ruang lingkup, 34 ADR, kontrak API, peta jalan, kriteria penyelesaian |
| [architecture.md](architecture.md) | Aturan dependensi, port/adapter, model pemrosesan serentak, siklus hidup |
| [data-model.md](data-model.md) | Skema SQLite, invarian, retensi, migrasi |

## Komunitas

Semua peserta proyek wajib mengikuti [Kode Etik Komunitas](../CODE_OF_CONDUCT.md), termasuk saat berdiskusi di issue, mengajukan pull request, dan meninjau kontribusi.

## Catatan legal

Mengunduh konten YouTube umumnya melanggar Terms of Service platform tersebut, dan hak cipta atas materi yang diunduh tetap pada pemiliknya. Proyek ini ditujukan sebagai alat lokal untuk penggunaan pribadi dan tidak menyediakan distribusi, berbagi, maupun penyimpanan konten daring. Konsekuensi praktisnya: yt-dlp rusak secara berkala mengikuti perubahan di sisi YouTube, sehingga perkakas sengaja dipisah dari siklus rilis aplikasi.

## Lisensi

[MIT](../LICENSE)

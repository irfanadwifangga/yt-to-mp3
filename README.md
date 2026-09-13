# yt-to-mp3

Konverter audio YouTube menjadi MP3 berbentuk aplikasi desktop-lokal: satu binary Go yang menjalankan server di loopback dan menyajikan SPA React yang tersemat di dalamnya. Tidak ada layanan cloud; semua pekerjaan dan data tinggal di mesinmu.

> **Status: fungsi inti dan hardening selesai.** Analisis, antrean, konversi, riwayat, setelan, pemasangan tool sekali klik, log, housekeeping, dan idle shutdown sudah berjalan, dan target NFR terukur lolos. Pipeline rilis sudah disiapkan, tetapi belum ada rilis yang diterbitkan dan binary belum ditandatangani. Baca [Keterbatasan yang diketahui](#keterbatasan-yang-diketahui) sebelum memakai.

## Fitur

- **Analisis tanpa mengunduh** — tempel tautan dan pratinjau judul, channel, durasi, serta codec sumber langsung muncul.
- **Progress live** per job lewat SSE, ditampilkan sebagai sampul video yang terisi warna dari bawah ke atas. Membatalkan job menghentikan seluruh process tree, termasuk FFmpeg yang dijalankan yt-dlp, lalu membersihkan berkas sementara.
- **Daftar selesai** — unduh hasil, buka foldernya di file manager, coba lagi yang gagal, atau hapus dari daftar (dengan atau tanpa berkasnya). Riwayat panjang dimuat bertahap.
- **Auto-retry** — kegagalan jaringan sementara diulang otomatis hingga 3 kali dengan jeda bertambah. Bila YouTube membatasi permintaan (HTTP 429), jedanya lebih panjang dan job paralel diturunkan ke satu selama beberapa menit.
- **Lima preset MP3**: 128, 192, 256, 320 kbps CBR, dan VBR V0. Keluaran 48 kHz stereo dengan tag ID3v2.3 dan sampul tersemat.
- **Setelan dalam dialog** (tombol **Setelan** atau `Ctrl+,`), dengan folder keluaran dipilih lewat dialog folder bawaan sistem operasi.
- **Bahasa Indonesia dan Inggris**, serta tema terang, gelap, atau mengikuti sistem.
- **Satu binary, satu instance** — menjalankannya dua kali membuka jendela yang sudah ada, bukan server kedua.

## Kebutuhan

| Perkakas | Versi | Dipakai untuk |
| --- | --- | --- |
| Go | 1.27+ | Membangun backend |
| Node.js | 24+ | Membangun SPA; tidak dibutuhkan saat aplikasi berjalan |
| yt-dlp | terbaru | Membaca metadata dan mengunduh audio |
| FFmpeg dan ffprobe | dengan `libmp3lame`; diuji pada 9.0.1 | Konversi dan verifikasi hasil |

Build pertama mengunduh modul Go dan paket npm, jadi butuh jaringan sekali. Seluruh dependensi Go murni Go (tanpa cgo), sehingga cross-compile tidak memerlukan toolchain C.

### Memasang yt-dlp dan FFmpeg

Cara termudah: buka **Setelan → Tool** lalu tekan **Pasang**. Aplikasi mengunduh versi yang di-pin di manifest, memverifikasi SHA-256 sebelum mengekstrak, lalu memasangnya ke direktori data aplikasi. Sumbernya yt-dlp dari rilis resminya, FFmpeg dari GyanD (Windows) dan martin-riedl.de (Linux, macOS); alasannya di [ADR-031](docs/yt-to-mp3-go-planning.md#4-keputusan-teknis).

Tool yang sudah terpasang lewat package manager juga dipakai; aplikasi menemukannya dari `PATH`.

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

Linux: pasang FFmpeg dari paket distro. Untuk yt-dlp, pakai binary dari [rilis resminya](https://github.com/yt-dlp/yt-dlp/releases) — paket distro sering tertinggal, padahal yt-dlp harus terus mengikuti perubahan di sisi YouTube.

Tool yang dipasang ke direktori yang sudah ada di `PATH` terdeteksi dalam 30 detik tanpa restart. Bila pemasangannya menambah direktori baru ke `PATH` — biasanya pada pemasangan pertama lewat winget — buka ulang aplikasi, karena proses yang sedang berjalan tidak melihat perubahan `PATH`.

## Mulai cepat

```bash
make install-web && make build && make run
```

Tanpa `make`:

```bash
npm --prefix web install && npm --prefix web run build && go build -o bin/yt-to-mp3 ./cmd/app && ./bin/yt-to-mp3
```

Aplikasi memilih port acak di `127.0.0.1` lalu membuka browser. Alamat pembukanya membawa session token sekali pakai; token itu langsung dipindahkan ke `sessionStorage` dan dihapus dari address bar.

Cara memakai:

1. Tempel URL video YouTube; analisis berjalan otomatis (atau tekan **Analisis** bila mengetik manual).
2. Pilih preset, lalu **Konversi**.
3. Pantau progress di **Sedang diproses**. Hasilnya pindah ke **Selesai** dan tersimpan di folder keluaran yang tertera di bagian bawah halaman.

Menghentikan aplikasi: tombol **Keluar** di kanan atas, atau `Ctrl+C` di terminal. Aplikasi juga berhenti sendiri setelah tab ditutup dan tidak ada job selama 30 menit; batasnya bisa diubah atau dimatikan (0) di **Setelan → Lanjutan**. Tidak ada tray icon karena pustaka tray membutuhkan cgo, dan itu akan merusak target cross-compile ([ADR-021](docs/yt-to-mp3-go-planning.md#4-keputusan-teknis)).

## Konfigurasi

Setelan dapat diubah dari dialog **Setelan** di UI, atau lewat `config.json` di direktori data (dibuat otomatis saat pertama kali dijalankan), atau lewat variabel lingkungan:

| Variabel | Setara dengan |
| --- | --- |
| `YT2MP3_OUTPUT_DIR` | `output_dir` |
| `YT2MP3_LOG_LEVEL` | `log_level` |
| `YT2MP3_MAX_CONCURRENT_JOBS` | `max_concurrent_jobs` |

Urutan prioritasnya: nilai bawaan, lalu `config.json`, lalu variabel lingkungan, lalu perubahan dari UI.

Hampir semua setelan berlaku seketika setelah disimpan dari UI, termasuk folder keluaran dan tingkat log. Pengecualiannya **jumlah job paralel**, yang baru berlaku setelah aplikasi dibuka ulang karena kapasitas worker disusun saat startup.

Folder keluaran dipilih lewat dialog folder bawaan sistem operasi. Dialog itu dibuka oleh server lokal, bukan browser, karena browser tidak pernah memberi tahu halaman web path absolut sebuah folder. Di Linux dialognya membutuhkan `zenity`, `kdialog`, atau `qarma`; tanpa salah satunya, ketik path folder secara manual. Folder baru diuji bisa ditulisi sebelum disimpan, dan konversi yang sedang berjalan tetap diselesaikan di folder sebelumnya.

## Pengembangan

Backend dan frontend dijalankan terpisah supaya hot reload Vite tetap hidup. Dua terminal:

```bash
make dev-api
```

```bash
make dev-web
```

Backend memakai port tetap 8799 di mode dev (produksi tetap acak) karena proxy Vite butuh target yang bisa ditebak. Flag `-dev` mengizinkan origin `localhost:5173` dan tidak pernah aktif pada build rilis.

Session token hanya bisa masuk lewat URL, jadi buka SPA dengan menempelkan token dari output terminal `make dev-api` (baris `buka:`). URL bertoken itu sengaja hanya dicetak ke terminal dan tidak pernah ke berkas log:

```
http://localhost:5173/?token=<token-dari-log>
```

### Pemeriksaan

| Perintah | Isi |
| --- | --- |
| `make check` | Format, `go vet`, dan seluruh test Go |
| `make typecheck` | Typecheck frontend |
| `make check-i18n` | Setiap kode error Go punya terjemahan `id` dan `en`, dan kunci kedua bahasa setara |
| `make test-integration` | Konversi dengan FFmpeg dan ffprobe sungguhan memakai fixture sintetis (nada sinus, sampul polos), ditambah E2E jalur job lengkap dengan yt-dlp palsu: konversi, auto-retry, dan pembatalan saat mengunduh. Gagal bila ffmpeg tidak ditemukan |
| `make nfr` | Ukur target [NFR](docs/yt-to-mp3-go-planning.md#22-non-functional-requirements) pada direktori data sementara; `URL="<tautan>"` ikut mengukur konversi dua job paralel |

Bentuk respons API dikunci berkas golden di `internal/api/testdata/golden/` dan ikut diperiksa `make test`. Perubahan kontrak yang disengaja ditulis ulang dengan `go test ./internal/api/ -run Kontrak -update`, lalu diff-nya ditinjau sebelum commit.

CI menjalankan seluruh pemeriksaan di atas kecuali `make nfr`, ditambah test Go di Windows dan macOS (terminasi process tree dan rename atomik berbeda per OS). Workflow **Regresi tool** memasang yt-dlp dan FFmpeg persis dari manifest di kelima target rilis lalu mengonversi fixture dengan binary hasil pasang; workflow itu jalan setiap kali kode tool atau manifest berubah, dan mingguan untuk menangkap URL hulu yang mati. `make help` menampilkan seluruh target.

Sebagian test menjalankan proses sungguhan — test terminasi process tree men-spawn proses anak dan cucu untuk membuktikan keduanya mati — sehingga paket `process` butuh beberapa detik.

### Rilis

Rilis dibangun GoReleaser dari tag `v*` lewat workflow **Rilis**: arsip untuk windows/amd64, linux/amd64, linux/arm64, darwin/amd64, dan darwin/arm64 dengan nama `yt-to-mp3_<versi>_<os>_<arch>`, beserta `checksums.txt`. Rilisnya dibuat sebagai **draft** dan baru terlihat publik setelah diterbitkan manual dari halaman Releases.

Sebelum tag pertama, validasi konfigurasinya dengan menjalankan workflow **Rilis** secara manual (mode snapshot, tidak membuat rilis), atau secara lokal bila goreleaser v2 terpasang:

```bash
make release-snapshot
```

Tool yt-dlp dan FFmpeg tidak pernah ikut di dalam arsip rilis; pengguna memasangnya dari aplikasi ([ADR-031](docs/yt-to-mp3-go-planning.md#4-keputusan-teknis)). Signing belum ada: macOS butuh codesign dan notarization dengan akun Apple Developer, Windows butuh sertifikat code signing.

## Struktur

```text
cmd/app/              entry point dan wiring dependensi
internal/
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
  browser/            membuka browser dan file manager
  version/            identitas build
web/                  SPA React + TypeScript, di-embed lewat go:embed
  src/locales/        kamus terjemahan id dan en
  scripts/            pemeriksa kelengkapan terjemahan
migrations/           migrasi SQL, di-embed
scripts/              pemutakhiran manifest tool
docs/                 perencanaan, arsitektur, data model
```

Arah impor satu arah: `api` dan `infrastructure` bergantung pada `application`, `application` hanya pada `domain`, dan `domain` tidak bergantung pada apa pun. Detailnya di [architecture.md](docs/architecture.md).

## API

Seluruh endpoint berada di bawah `/api` dan mewajibkan header `X-Session-Token`, kecuali `GET /api/ping` yang dipakai untuk mendeteksi instance yang sudah berjalan. Kontrak lengkapnya di [planning §7](docs/yt-to-mp3-go-planning.md#7-kontrak-api).

| Endpoint | Fungsi |
| --- | --- |
| `GET /health` | Status aplikasi, tool, dan antrean |
| `GET /tools` · `POST /tools/install` | Status dan instalasi tool |
| `POST /tools/check` · `POST /tools/update` | Cek versi terbaru; perbarui yt-dlp ke rilis terbaru |
| `GET /presets` | Daftar preset |
| `POST /metadata` | Analisis URL tanpa mengunduh |
| `POST /jobs` · `GET /jobs` · `GET /jobs/{id}` | Buat, daftar, dan baca job |
| `GET /jobs/{id}/events` | Progress live (SSE, mendukung `Last-Event-ID`) |
| `POST /jobs/{id}/cancel` · `POST /jobs/{id}/retry` · `DELETE /jobs/{id}` | Batalkan, ulangi, hapus (`?delete_file=true` ikut menghapus berkas hasil) |
| `GET /files/{id}` · `POST /files/{id}/reveal` | Unduh hasil, buka lokasinya |
| `GET /settings` · `PUT /settings` | Baca dan ubah setelan |
| `POST /dialogs/folder` | Buka dialog pemilih folder native |
| `POST /shutdown` | Hentikan aplikasi |

## Lokasi data

| OS | Data | Keluaran bawaan |
| --- | --- | --- |
| Windows | `%LOCALAPPDATA%\yt-to-mp3\` | `%USERPROFILE%\Music\yt-to-mp3\` |
| macOS | `~/Library/Application Support/yt-to-mp3/` | `~/Music/yt-to-mp3/` |
| Linux | `$XDG_DATA_HOME/yt-to-mp3/` | `$XDG_MUSIC_DIR` atau `~/Music/yt-to-mp3/` |

Direktori data berisi database SQLite (`db/app.db`), `config.json`, tool yang dipasang aplikasi, berkas sementara, log, dan `runtime.json` selama aplikasi berjalan.

Log ditulis ke `logs/app.log` sekaligus ke terminal, dirotasi harian menjadi `logs/app-YYYY-MM-DD.log`, dan arsip yang lebih tua dari 7 hari dibuang. Lampirkan berkas ini saat melaporkan bug; token sesi tidak pernah tertulis di sana.

Aplikasi merawat datanya sendiri saat dibuka lalu setiap jam: jejak event job yang selesai lebih dari 30 hari dipangkas (job dan berkasnya tetap ada), cache metadata kedaluwarsa dibuang, berkas sementara yatim dibersihkan, dan status berkas hasil disamakan dengan disk. Berkas yang dihapus di luar aplikasi ditandai hilang, dan tersedia lagi bila muncul kembali, misalnya setelah drive eksternal dicolok ulang.

## Keamanan

Server lokal bukan berarti server privat: situs web mana pun yang sedang dibuka pengguna dapat mengirim request ke `127.0.0.1`. Karena itu server ini menerapkan bind loopback eksplisit, allowlist header `Host` sebagai penangkal DNS rebinding, pemeriksaan `Origin` pada setiap request yang mengubah state, session token acak per proses, CORS deny-all, dan batas ukuran body. Path berkas tidak pernah diterima dari klien, dan argumen tool tidak pernah melewati shell. Perinciannya di [planning §15](docs/yt-to-mp3-go-planning.md#15-model-keamanan-lokal).

## Keterbatasan yang diketahui

- **FFmpeg yang dipasang aplikasi hanya maju lewat rilis aplikasi.** yt-dlp bisa diperbarui langsung dari **Setelan → Tool** (diverifikasi dengan `SHA2-256SUMS` rilis resminya), tetapi FFmpeg tetap mengikuti manifest yang di-pin; untuk memajukannya, jalankan `make update-tools` lalu commit.
- **Belum ada rilis biner yang diterbitkan**; untuk sekarang build dari source. Binary rilis nantinya belum ditandatangani, sehingga SmartScreen di Windows dan Gatekeeper di macOS akan memperingatkan saat pertama dijalankan.

Sengaja tidak didukung: playlist, siaran langsung, video yang butuh login atau dibatasi usia, dan keluaran video. Daftar lengkapnya di [non-goals](docs/yt-to-mp3-go-planning.md#3-non-goals).

## Dokumentasi

| Dokumen | Isi |
| --- | --- |
| [yt-to-mp3-go-planning.md](docs/yt-to-mp3-go-planning.md) | Ruang lingkup, 33 ADR, kontrak API, roadmap, kriteria selesai |
| [architecture.md](docs/architecture.md) | Aturan dependensi, port/adapter, model concurrency, siklus hidup |
| [data-model.md](docs/data-model.md) | Skema SQLite, invarian, retensi, migrasi |

## Catatan legal

Mengunduh konten YouTube umumnya melanggar Terms of Service platform tersebut, dan hak cipta atas materi yang diunduh tetap pada pemiliknya. Proyek ini ditujukan sebagai alat lokal-personal dan tidak menyediakan distribusi, sharing, maupun hosting konten. Konsekuensi praktisnya: yt-dlp rusak secara berkala mengikuti perubahan di sisi YouTube, sehingga tool sengaja dipisah dari siklus rilis aplikasi.

## Lisensi

[MIT](LICENSE)

# YT → MP3 Converter — Technical Planning

Dokumen perencanaan teknis untuk aplikasi desktop-local berbasis Go + embedded SPA.

> **Revisi 4 (2026-09-12).** Sample rate output pindah dari 44.1 kHz ke 48 kHz (ADR-030), menyesuaikan sumber YouTube yang didominasi Opus.
>
> **Revisi 3 (2026-09-12).** Menambahkan non-goals, ADR-021..ADR-029, NFR, definisi `filename_mode`, timeout berbasis durasi, preflight disk, penolakan livestream, dan kebijakan lokalisasi error. Desain internal dipisah ke dokumen tersendiri. Revisi 2 memperbaiki tiga keputusan yang salah bentuk: persistensi event progress, pembatasan koneksi SQLite, dan timeout konstan.

## 1. Peta dokumen

| Dokumen | Isi |
| --- | --- |
| **yt-to-mp3-go-planning.md** (ini) | Ruang lingkup, keputusan, kontrak eksternal, roadmap, kriteria selesai |
| [architecture.md](architecture.md) | Aturan dependensi, port/adapter, model concurrency, siklus hidup aplikasi, housekeeping |
| [data-model.md](data-model.md) | DDL SQLite, invarian, retensi, migrasi |

## 2. Target arsitektur

- Go sebagai runtime/server lokal
- React + TypeScript + Vite sebagai SPA statis
- SPA di-embed dengan `embed.FS`
- HTTP JSON + SSE, loopback-only
- SQLite lokal via driver pure-Go (non-CGO)
- In-process bounded worker pool
- yt-dlp untuk extraction/download
- FFmpeg untuk transcoding dan tagging
- MP3 sebagai output utama MVP

## 3. Non-goals

Ditulis eksplisit supaya tidak diam-diam masuk lewat scope creep:

| Bukan tujuan | Catatan |
| --- | --- |
| Playlist dan channel | Ditunda; menuntut relasi parent-child pada data model (ADR-017) |
| Output video | Aplikasi ini hanya menghasilkan audio |
| Login, cookies, bypass age-gate | `AGE_RESTRICTED` berhenti sebagai error, bukan fitur tertunda |
| Akses multi-user atau remote | Loopback-only adalah keputusan keamanan, bukan keterbatasan sementara |
| Prioritas antrean dan penjadwalan | Antrean FIFO sederhana |
| Sinkronisasi cloud dan pustaka media | Di luar cakupan alat lokal |
| Editor tag manual | Tagging otomatis dari metadata source saja |
| Raw FFmpeg args dari user | Preset adalah product contract |

## 4. Keputusan teknis

| ID | Keputusan | Alasan |
| --- | --- | --- |
| ADR-001 | Go | Binary tunggal, concurrency, process management |
| ADR-002 | embedded SPA | Tidak butuh Node/Python saat runtime |
| ADR-003 | SQLite | Local-first, zero external DB |
| ADR-004 | SSE | Progress satu arah server → browser |
| ADR-005 | Bounded worker pool | Back-pressure dan kontrol resource |
| ADR-006 | yt-dlp + FFmpeg subprocess | Separation of concerns dan tool maturity |
| ADR-007 | Atomic output commit | Hindari partial final file |
| ADR-008 | Idempotent retry | Hindari duplicate/korup output |
| ADR-009 | MP3 canonical MVP | UX sederhana dan sesuai tujuan produk |
| ADR-010 | ~~Embedded tools sebagai target packaging~~ **digantikan ADR-013** | Lihat ADR-013 |
| ADR-011 | Driver `modernc.org/sqlite` (pure Go) | Cross-compile tanpa toolchain C; prasyarat rilis single-binary multi-platform |
| ADR-012 | Process group (Unix) / Job Object (Windows) | `Process.Kill()` hanya membunuh anak langsung; ffmpeg anak yt-dlp akan jadi zombie dan mengunci file temp |
| ADR-013 | Tool acquisition on-first-run + checksum, sidecar/PATH sebagai fallback | yt-dlp rusak berkala mengikuti perubahan YouTube; embedding memaksa rilis ulang aplikasi dan membengkakkan binary ~110MB per platform |
| ADR-014 | Loopback-only + validasi Host/Origin + session token | Server di localhost dapat di-request situs web mana pun; mitigasi CSRF dan DNS rebinding |
| ADR-015 | `-f bestaudio` lalu transcode eksplisit, bukan `yt-dlp -x` end-to-end | Hindari mengunduh stream video; kontrol penuh atas parameter encoder dan progress |
| ADR-016 | Progress berbobot dua fase | Download dan transcode punya sumber progress berbeda dan tidak sebanding |
| ADR-017 | `--no-playlist` pada MVP | Playlist menuntut relasi parent-child pada data model; ditunda agar MVP tetap sederhana |
| ADR-018 | `--ignore-config`, argv tanpa shell, whitelist skema URL | Cegah injeksi lewat `yt-dlp.conf` milik user dan URL yang terbaca sebagai flag |
| ADR-019 | Migrasi versioned dengan `goose` + `embed.FS` | Migrasi terkontrol tanpa dependensi eksternal saat runtime |
| ADR-020 | ID3v2.3 tagging + embedded cover art di pipeline | Output MP3 tanpa tag terasa belum selesai untuk use case musik |
| ADR-021 | Tanpa tray icon; keluar lewat tombol di SPA + idle shutdown | Pustaka tray umumnya butuh cgo dan bertabrakan dengan ADR-011 |
| ADR-022 | Single instance via lock file + `runtime.json` | Cegah dua worker pool pada satu database, sekaligus jalan menemukan port acak |
| ADR-023 | Event `progress` tidak dipersist | 4 event/detik menghasilkan ribuan baris per job dan memperlambat replay SSE |
| ADR-024 | Handle baca dan tulis SQLite dipisah | WAL mengizinkan pembaca konkuren; satu pool tunggal menyerialkan pembacaan tanpa perlu |
| ADR-025 | Timeout fase diturunkan dari durasi media | Konstanta 30 menit membunuh konten panjang yang sah |
| ADR-026 | Livestream ditolak eksplisit | Tidak berdurasi dan tidak pernah selesai; akan selalu berakhir di timeout |
| ADR-027 | Lokalisasi di client berdasarkan `error.code` | Mencegah dua sumber kebenaran untuk teks yang sama |
| ADR-028 | Tabel `presets` bersifat append-only | `preset_id` tersimpan permanen di history dan jadi kontrak selamanya |
| ADR-029 | Reservasi nama file atomik dengan `O_EXCL` | Pengecekan "file ada?" yang naif membuat dua worker memenangkan nama yang sama |
| ADR-030 | Sample rate output 48 kHz, bukan 44.1 kHz | Sumber YouTube didominasi Opus yang secara desain selalu 48 kHz; 44.1 memaksa resampling pada jalur paling umum. MPEG-1 Layer III mendukung 48 kHz secara native, jadi tidak ada kompromi kompatibilitas |
| ADR-031 | Binary FFmpeg diunduh saat runtime dari rilis berversi: GyanD (Windows) dan martin-riedl.de (Linux, macOS) | Proyek FFmpeg tidak mendistribusikan build statis resmi. Mengunduh saat runtime membuat rilis kita tidak pernah menjadi distributor FFmpeg, sehingga kewajiban LGPL/GPL tidak menempel pada artifact rilis. Keduanya dirujuk halaman unduhan ffmpeg.org. Rencana awal (BtbN dan evermeet.cx) ditinggalkan: BtbN hanya menyediakan snapshot master yang dirotasi, sehingga checksum ter-pin basi, dan evermeet.cx tidak punya build arm64 |
| ADR-032 | Dependensi pure-Go `github.com/ulikunitz/xz` | Build FFmpeg untuk Linux hanya tersedia sebagai `.tar.xz` dan stdlib tidak punya dekoder xz. Paket ini pure Go sehingga ADR-011 tetap terjaga |
| ADR-033 | Manifest tool bersifat fail-closed | Checksum kosong menolak instalasi. Lebih baik fitur tidak jalan daripada menjalankan binary pihak ketiga tanpa verifikasi. **Pengecualian yt-dlp:** atas permintaan eksplisit pengguna, yt-dlp boleh diperbarui ke rilis terbaru dengan checksum dari `SHA2-256SUMS` rilis yang sama (seperti `yt-dlp -U`), karena yt-dlp rusak mengikuti perubahan YouTube lebih cepat daripada siklus rilis aplikasi. Verifikasi tetap wajib; yang berubah hanya asal checksum. FFmpeg tidak mendapat pengecualian ini |

## 5. Struktur folder

```text
yt-to-mp3/
├─ cmd/app/main.go
├─ internal/
│  ├─ api/                 # handler, middleware (security, recover), SSE hub
│  ├─ application/         # use case, port, orkestrasi job
│  ├─ domain/              # entity, state machine, taksonomi error
│  ├─ infrastructure/
│  │  ├─ db/               # koneksi, repository, runner migrasi
│  │  ├─ ytdlp/            # arg builder, progress parser, error mapper
│  │  ├─ ffmpeg/           # arg builder, progress parser, tagging
│  │  ├─ process/          # runner + killer (proc_unix.go / proc_windows.go)
│  │  ├─ fs/               # atomic commit, GC temp, sanitizer, resolusi path
│  │  └─ tools/            # discovery, fetch, verifikasi checksum, version pin
│  ├─ config/              # load/merge config, default per OS
│  └─ worker/              # pool, queue, scheduler
├─ web/
│  ├─ src/
│  └─ dist/                # output build, di-embed (placeholder di-commit)
├─ migrations/
├─ testdata/
├─ scripts/
├─ docs/
├─ .github/workflows/
├─ Makefile
├─ go.mod
└─ README.md
```

## 6. Job lifecycle

`queued → resolving → downloading → converting → verifying → completed`

Failure dari state aktif dapat menjadi `failed`; user cancellation menjadi `cancelling → cancelled`.

Aturan:

- `cancelling` punya grace period: sinyal terminasi lembut → tunggu 5 detik → kill paksa seluruh process tree (§14).
- State non-terminal yang ditemukan saat startup diperlakukan sebagai interupsi, bukan state valid (§18).
- Transisi hanya boleh lewat satu fungsi state machine di `domain/`; repository tidak menulis `status` secara langsung.
- Setiap transisi menulis baris `job_events` dalam transaksi yang sama dengan update `jobs`.
- Handler HTTP tidak pernah menulis status terminal; ia hanya memicu, job runner yang menutup.

## 7. Kontrak API

Semua endpoint berada di bawah middleware keamanan pada §15.

### `GET /api/health`

Status app, versi app, ketersediaan + versi tool, dan ringkasan antrean (job aktif, kedalaman antrean).

### `GET /api/presets`

Daftar preset non-deprecated untuk mengisi UI. SPA tidak boleh meng-hardcode preset.

### `POST /api/metadata`

Request:

```json
{ "url": "https://example.invalid/video" }
```

Hasil di-cache di `media_items` berdasarkan `source_key` dengan TTL 24 jam. Respons menyertakan `is_live`; UI menonaktifkan tombol convert bila bernilai benar.

Respons juga menyertakan `previous_conversions`, yaitu job `completed` untuk `source_key` yang sama yang berkasnya masih tercatat ada. Isinya `job_id`, `preset_id`, `file_id`, `file_name`, dan `finished_at`, terbaru lebih dulu; nilainya selalu array, tidak pernah null. Bila preset yang dipilih pernah dipakai, UI menampilkan peringatan dengan tombol **Tampilkan di folder** dan mengganti label tombol menjadi **Konversi lagi**, supaya konversi ulang tidak diam-diam menghasilkan `Judul (2).mp3`. Kegagalan membaca riwayat ini tidak menggagalkan analisis.

Respons juga menyertakan `suggested_title` dan `suggested_artist`, tebakan tag yang rapi dari `domain.SuggestTags`: penanda jenis unggahan di dalam kurung atau setelah `|` dibuang (`(Official Video)`, `[MV]`, `| Official Lyric Video`), judul berbentuk `Artis - Judul` dengan tepat satu pemisah dipecah, dan akhiran kanal `- Topic` serta `VEVO` dibuang dari uploader. Sebuah kelompok hanya dibuang bila **setiap** katanya penanda dan minimal satu penanda kuat, sehingga `(4K Remaster)` dan `(Official Video Remastered)` dibuang, sedangkan `(Remix)`, `(Live)`, `(feat. X)`, `(2019 Remaster)`, dan `(Remastered)` membedakan rekaman dan tetap dipertahankan. Kasus ujinya diambil dari judul YouTube nyata. Saran ini hanya mengisi formulir Judul dan Artis di pratinjau; bila berbeda dari metadata asli, UI menampilkan judul asli dengan tombol **Pakai data asli**, karena tebakan seperti "Episode 5 - Penutup" bisa keliru.

### `POST /api/jobs`

Request:

```json
{
  "url": "https://example.invalid/video",
  "preset_id": "mp3_standard",
  "filename_mode": "title",
  "title": "Bohemian Rhapsody",
  "artist": "Queen"
}
```

Response `202`:

```json
{ "id": "job_abc123", "status": "queued", "preset_id": "mp3_standard", "progress": 0 }
```

- Menolak dengan `DUPLICATE_ACTIVE_JOB` bila ada job non-terminal dengan `source_key` + `preset_id` yang sama.
- Menolak dengan `QUEUE_FULL` bila antrean penuh.
- `title` dan `artist` opsional; kosong berarti memakai metadata sumber. Nilainya dirapikan `domain.CleanTag` (karakter kontrol dibuang, spasi diringkas), disimpan per job di `jobs.tag_title`/`tag_artist`, dan dipakai untuk tag ID3 **sekaligus** nama berkas. Lebih dari 200 karakter ditolak `BAD_REQUEST`, bukan dipotong diam-diam. `title` juga menjadi judul riwayat; retry manual mewarisi keduanya.
- Konversi selesai sebelumnya tidak dilaporkan di sini, melainkan di `previous_conversions` saat analisis, supaya peringatannya muncul sebelum pengguna menekan Konversi.

### `GET /api/jobs/:id`

Status snapshot job.

### `GET /api/jobs/:id/events`

SSE progress stream. Detail di §8.

### `GET /api/jobs`

History dengan pagination cursor-based (`limit` maks 100), filter opsional `status`.

### `POST /api/jobs/:id/cancel`

Memulai graceful cancellation. Idempoten: job terminal mengembalikan `409`.

### `POST /api/jobs/:id/retry`

Membuat percobaan baru untuk job `failed`/`cancelled`. Menolak untuk kelas error permanen (§19).

### `DELETE /api/jobs/:id`

Menghapus job dari history. Query `?delete_file=true` ikut menghapus file output.

### `GET /api/files/:id`

Mengirim file final tanpa mengekspos path filesystem. `Content-Disposition: attachment`.

### `POST /api/files/:id/reveal`

Membuka file manager pada lokasi file. Hanya menerima id yang terdaftar di tabel `files`; tidak pernah menerima path dari client.

### `GET /api/settings` / `PUT /api/settings`

Output directory, concurrency, preset default, filename mode default, kapasitas antrean, tingkat log. Setiap baris membawa `kind` (`path`, `int`, `enum`, `bool`, `string`) dan `requires_restart`.

- `PUT` memvalidasi seluruh kunci lebih dulu, lalu **menerapkan** kunci yang punya applier (`output_dir`, `log_level`) sebelum menyimpan. Applier yang menolak — misalnya folder yang tidak dapat ditulisi — menghasilkan `INVALID_SETTING` dengan `details.key`, dan tidak ada yang tersimpan. Bila penyimpanan gagal setelah applier berjalan, nilai lama dipasang kembali.
- `output_dir` wajib path absolut. Job mengambil snapshot direktori keluaran saat mulai, sehingga reservasi nama, berkas sementara, dan rename akhir tetap sevolume walau folder diganti di tengah konversi.
- Hanya `max_concurrent_jobs` yang `requires_restart`. Nilai tersimpan dibaca **sebelum** store, scheduler, dan logger disusun saat startup; folder tersimpan yang tidak lagi dapat dipakai jatuh ke bawaan config dengan peringatan di log, bukan menggagalkan startup.
- Kunci yang belum punya implementasi (`tool_update_check`) tidak dikembalikan.
- `idle_shutdown_minutes` (0–1440, 0 mematikan) dibaca ulang setiap pemeriksaan idle. Aplikasi berhenti bila tidak ada job aktif atau antre, tidak ada stream SSE, dan tidak ada request **bertoken** selama batas itu; jam idle dihitung dari request terakhir atau saat terakhir aplikasi sibuk, mana yang lebih baru. Request tanpa token tidak dihitung, supaya situs lain tidak dapat menjaga aplikasi tetap hidup. Dimatikan pada mode `-dev`.

### `POST /api/dialogs/folder`

Body `{"title": "...", "start": "..."}` (keduanya opsional). Membuka dialog pemilih folder native di desktop pengguna dan menahan respons sampai dialog ditutup: `{"path": "C:\\Users\\...\\Music", "cancelled": false}` atau `{"cancelled": true}`.

- Browser tidak pernah mengungkap path absolut folder ke halaman web, jadi dialog dibuka oleh proses server (`ncruces/zenity`, murni Go di Windows dan macOS; di Linux memanggil `zenity`/`kdialog`/`qarma`).
- Endpoint ini tidak membaca atau menulis apa pun; path hasil pilihan baru berlaku setelah dikirim lewat `PUT /api/settings`, yang memvalidasinya.
- Dijaga token dan Origin seperti endpoint lain: tanpa itu situs mana pun dapat memunculkan dialog di layar pengguna. Satu dialog pada satu waktu.
- Tanpa implementasi dialog di sistem: `503 DIALOG_UNAVAILABLE`, dan UI jatuh ke input teks.

### `POST /api/shutdown`

Menghentikan aplikasi secara graceful. Inilah tombol Quit di SPA (ADR-021).

### `filename_mode`

Set tertutup. Nilai default `title`.

| Nilai            | Hasil                            |
| ---------------- | -------------------------------- |
| `title`          | `Judul Video.mp3`                |
| `title-uploader` | `Judul Video - Nama Channel.mp3` |
| `uploader-title` | `Nama Channel - Judul Video.mp3` |
| `id`             | `dQw4w9WgXcQ.mp3`                |

Template kustom adalah non-goal MVP.

## 8. Kontrak SSE

- Event bernama: `state`, `progress`, `log`, `done`, `error`. Setiap event punya `id` monoton naik dari satu counter per job.
- Hanya `state`, `error`, dan `done` yang dipersist ke `job_events` dan bisa di-replay (ADR-023). `progress` hidup di memori.
- Heartbeat komentar (`: ping`) tiap 15 detik agar koneksi tidak ditutup idle timeout.
- `Last-Event-ID`: pada reconnect, server mengirim event terpersist yang terlewat, ditambah **satu** snapshot progress terkini — bukan replay ribuan frame lama.
- Subscriber terlambat: job yang sudah terminal menerima snapshot state lalu stream ditutup. Tidak pernah menggantung.
- Disconnect: handler wajib memantau `r.Context().Done()` dan melepas subscriber.
- Batas 4 koneksi SSE per job; lebih dari itu ditolak `429`.
- Throttle `progress` maksimum 4 event/detik per job; perubahan `state` selalu dikirim tanpa throttle.

Urutan subscribe yang benar (mendaftar listener sebelum membaca histori, agar tidak ada event yang jatuh di celah) dijelaskan di [architecture.md §6](architecture.md).

## 9. Kode error dan lokalisasi

```json
{ "error": { "code": "INVALID_URL", "message": "URL tidak dapat diproses.", "details": {} } }
```

Daftar `code` bersifat tertutup dan menjadi bagian kontrak:

`INVALID_URL`, `INVALID_SETTING`, `UNSUPPORTED_URL`, `LIVE_NOT_SUPPORTED`, `VIDEO_UNAVAILABLE`, `VIDEO_PRIVATE`, `GEO_BLOCKED`, `AGE_RESTRICTED`, `RATE_LIMITED`, `TOOL_MISSING`, `TOOL_OUTDATED`, `TOOL_INSTALL_FAILED`, `TOOL_MANIFEST_INCOMPLETE`, `TOOL_CHECKSUM_MISMATCH`, `DOWNLOAD_FAILED`, `TRANSCODE_FAILED`, `VERIFY_FAILED`, `DISK_FULL`, `OUTPUT_WRITE_FAILED`, `DIALOG_UNAVAILABLE`, `JOB_NOT_FOUND`, `QUEUE_FULL`, `DUPLICATE_ACTIVE_JOB`, `INTERRUPTED`, `CANCELLED`, `TIMEOUT`, `INTERNAL`.

Lapisan HTTP menambahkan kode transport tersendiri yang tidak dimiliki domain: `FORBIDDEN_HOST`, `FORBIDDEN_ORIGIN`, `UNAUTHORIZED`, `UNSUPPORTED_MEDIA_TYPE`, `BAD_REQUEST`, `NOT_FOUND`.

Aturan (ADR-027):

- **Client yang menerjemahkan**, berdasarkan `code`. Field `message` adalah fallback untuk developer dan log, bukan teks yang ditampilkan.
- Pesan mentah dari yt-dlp/FFmpeg tidak pernah dikirim ke UI; disimpan di log dan `job_events`.
- Field `details` boleh memuat konteks terstruktur yang dibutuhkan klien untuk menunjuk sumber masalah, misalnya `{"key": "max_concurrent_jobs"}` pada `INVALID_SETTING`. Isinya kunci dan nilai, tidak pernah kalimat siap tampil — itu tetap dirakit klien dari `code`.
- Menambah `code` baru adalah perubahan kontrak: butuh entri terjemahan di SPA pada commit yang sama. Aturan ini ditegakkan `npm --prefix web run check:i18n`, yang membaca daftar kode langsung dari sumber Go dan menggagalkan CI bila ada kode tanpa terjemahan `id` maupun `en`, bila kunci kedua bahasa tidak setara, atau bila komponen memakai kunci yang tidak ada.
- Bahasa UI MVP: Indonesia dan Inggris, dengan Inggris sebagai fallback.

## 10. Model data

Ringkasan; DDL lengkap, invarian, dan retensi ada di [data-model.md](data-model.md).

| Tabel         | Peran                                               |
| ------------- | --------------------------------------------------- |
| `jobs`        | Satu baris per permintaan konversi                  |
| `media_items` | Cache metadata source, dikunci `source_key`         |
| `files`       | Output final, satu per job                          |
| `job_events`  | Event terpersist untuk observability dan replay SSE |
| `presets`     | Definisi format, append-only                        |
| `settings`    | Override konfigurasi dari user                      |
| `schema_meta` | Versi skema, penjaga rollback                       |

## 11. Matriks format dan preset

### MVP

| Preset id      | Label    | Mode | Parameter                          |
| -------------- | -------- | ---- | ---------------------------------- |
| `mp3_economy`  | Economy  | CBR  | 128 kbps, 48 kHz stereo          |
| `mp3_standard` | Standard | CBR  | 192 kbps, 48 kHz stereo          |
| `mp3_high`     | High     | CBR  | 256 kbps, 48 kHz stereo          |
| `mp3_max`      | Max      | CBR  | 320 kbps, 48 kHz stereo          |
| `mp3_vbr_v0`   | VBR High | VBR  | `vbr_quality = 0`, 48 kHz stereo |

Nilai V0 hanya hidup di kolom `presets.vbr_quality` dan diterjemahkan jadi `-q:a 0` saat membangun argv — tidak ditulis ulang di tempat lain.

### Tahap lanjutan

M4A/AAC 192, Opus 160, FLAC lossless, WAV PCM 16-bit.

Preset lossless (FLAC, WAV) memakai `sample_rate = NULL` yang berarti **ikuti sumber**. Memaksa sample rate apa pun pada preset lossless menghilangkan alasan keberadaannya: resample ke 44.1 kHz maupun upsample ke 48 kHz sama-sama membuat file tidak lagi menjadi salinan setia dari keluaran decoder. Preset MP3 tetap 48 kHz tetap, karena formatnya memang lossy dan yang dikejar hanya menghindari resample sia-sia pada mayoritas kasus.

Preset adalah product contract; user tidak memasukkan raw FFmpeg args pada MVP.

## 12. Pipeline eksekusi

### 12.1 Resolving dan download

Argumen yt-dlp dibangun sebagai argv, tidak pernah lewat shell:

```text
--ignore-config          # abaikan yt-dlp.conf milik user (vektor injeksi --exec)
--no-exec
--no-playlist            # ADR-017
--newline
--progress-template "download:PROGRESS %(progress.downloaded_bytes)s %(progress.total_bytes_estimate)s"
-f "bestaudio/best"      # ADR-015, jangan unduh stream video
--write-thumbnail --convert-thumbnail jpg
--retries 2
--socket-timeout 30
-o "<tmp>/<job_id>.%(ext)s"
--                       # akhiri parsing flag sebelum URL
<url>
```

Metadata diambil dengan `--dump-single-json --skip-download` memakai flag hardening yang sama. Field `is_live` diperiksa di sini; bernilai benar berarti job ditolak `LIVE_NOT_SUPPORTED` sebelum ada byte yang diunduh (ADR-026).

### 12.2 Transcode dan tagging

```text
ffmpeg -hide_banner -nostdin -y
  -i <audio_input>
  -i <cover.jpg>                           # opsional
  -map 0:a:0 -map 1:v:0
  -c:a libmp3lame -b:a 192k -ar 48000 -ac 2
  -filter:v:0 crop=min(iw\,ih):min(iw\,ih),scale=min(iw\,800):min(ih\,800)
  -c:v mjpeg -q:v 2 -disposition:v:0 attached_pic
  -id3v2_version 3                         # v2.3 paling luas didukung player
  -metadata title=<title> -metadata artist=<artist> -metadata album=<artist>
  -metadata comment=<source_url>
  -progress pipe:1 -nostats
  <tmp_out>.mp3
```

Preset VBR memakai `-q:a <vbr_quality>` menggantikan `-b:a`.

`<title>` dan `<artist>` adalah suntingan pengguna bila ada (§7 `POST /api/jobs`), selain itu judul dan uploader dari metadata. Tag kosong dilewati, bukan ditulis kosong. Tag `date` belum ditulis karena metadata yang dinormalkan belum membawa tanggal unggah.

Cover art: thumbnail hasil `--write-thumbnail` dipotong persegi di tengah lalu diperkecil ke maksimum 800×800 sebelum disematkan. Thumbnail YouTube berbentuk 16:9 sedangkan pemutar musik menampilkan sampul persegi, dan artwork unggahan musik hampir selalu di tengah bingkai; konsekuensinya, sisi kiri-kanan thumbnail video biasa ikut terpotong. `-q:v 2` dipakai karena bitrate bawaan mjpeg membuat sampul tampak pecah. Ukuran berkas thumbnail tidak dibatasi terpisah: hasil akhirnya selalu di-encode ulang ke ≤ 800×800.

Kegagalan pada jalur cover art **tidak pernah** menggagalkan job. Bila FFmpeg gagal dengan `TRANSCODE_FAILED` saat sampul disertakan, `Transcoder` mengulang konversi sekali tanpa sampul dan mencatat peringatan; timeout dan pembatalan tidak diulang. Test integrasi `TestIntegrasiSampulRusakTidakMenggagalkan` memakai berkas `.jpg` yang bukan gambar.

### 12.3 Model progress

| Fase        | Rentang | Sumber                                    |
| ----------- | ------- | ----------------------------------------- |
| resolving   | 0–5%    | step-based                                |
| downloading | 5–70%   | `downloaded_bytes / total_bytes_estimate` |
| converting  | 70–95%  | `out_time_ms / duration_ms`               |
| verifying   | 95–100% | step-based                                |

Bila ukuran total atau durasi tidak diketahui, `progress` dikirim `null` dan UI menampilkan indikator indeterminate. Jangan pernah mengirim `NaN` atau menahan nilai di 0.

### 12.4 Verifikasi dan commit

1. `ffprobe` memastikan file ada, berukuran > 0, punya stream audio, dan durasinya dalam toleransi ±2 detik dari metadata source.
2. Hitung `sha256`, simpan ke `files`.
3. Menangkan nama final lewat reservasi `O_EXCL` (ADR-029), lalu `os.Rename` dari temp.

Temp untuk commit final berada di `<output_dir>/.tmp/` — **satu volume dengan output**, karena `os.Rename` tidak atomik, dan gagal di Windows, antar-volume. Temp unduhan mentah boleh berada di app data dir.

## 13. Timeout, batas, dan preflight

Timeout diturunkan dari durasi media, bukan konstanta (ADR-025):

| Fase        | Timeout                     |
| ----------- | --------------------------- |
| resolving   | 60 detik                    |
| downloading | `max(10 menit, durasi × 3)` |
| converting  | `max(5 menit, durasi × 1)`  |
| verifying   | 60 detik                    |

Preflight disk sebelum download dimulai:

```text
estimasi = ukuran_unduhan + (durasi_detik × bitrate_preset / 8)
butuh    = estimasi × 1.5
```

Ruang kurang berarti gagal cepat dengan `DISK_FULL`, bukan mati di 95%.

Batas lain:

| Batas               | Nilai default             |
| ------------------- | ------------------------- |
| Job konkuren        | 2                         |
| Kedalaman antrean   | 50                        |
| Koneksi SSE per job | 4                         |
| Ukuran sampul       | persegi, maks 800×800     |
| Body request API    | 64 KB                     |
| Panjang URL         | 2048 karakter             |

## 14. Process management dan cancellation

`Process.Kill()` hanya membunuh anak langsung. yt-dlp men-spawn ffmpeg sendiri untuk muxing, jadi tanpa penanganan process tree, cancel meninggalkan proses yatim yang masih mengunci file temp — dan di Windows file terkunci tidak bisa dihapus, sehingga cleanup ikut gagal.

```go
// proc_unix.go
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
// terminasi: syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
//            tunggu grace, lalu syscall.Kill(-pgid, syscall.SIGKILL)

// proc_windows.go
// 1. CreateJobObjectW
// 2. SetInformationJobObject(JobObjectExtendedLimitInformation,
//                            JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE)
// 3. cmd.SysProcAttr = &syscall.SysProcAttr{
//        CreationFlags: CREATE_SUSPENDED | CREATE_NEW_PROCESS_GROUP}
// 4. AssignProcessToJobObject(job, processHandle)
// 5. ResumeThread(mainThread)
// terminasi: CloseHandle(job) -> seluruh tree mati
```

Aturan:

- Setiap job memegang satu handle job object / pgid, disimpan di memori worker, tidak pernah di DB.
- Cancel menutup handle, **menunggu `Wait()` kembali**, baru menghapus file temp. Urutan ini wajib.
- Shutdown aplikasi membatalkan seluruh job aktif lewat jalur yang sama sebelum keluar.
- Timeout per fase (§13) memakai jalur terminasi yang sama.

## 15. Model keamanan lokal

Ancaman utama bukan shell injection, melainkan bahwa server HTTP di localhost dapat di-request oleh **situs web mana pun yang kebetulan dibuka user**. DNS rebinding melewati pembatasan origin biasa.

| Kontrol | Implementasi |
| --- | --- |
| Bind | `127.0.0.1:0` eksplisit (port acak). Jangan `:8080` — itu berarti `0.0.0.0` |
| Host allowlist | Tolak request yang header `Host`-nya bukan `127.0.0.1:<port>` atau `localhost:<port>` |
| Origin check | Semua request non-GET wajib `Origin` yang cocok; selain itu `403` |
| Content-Type | Endpoint tulis hanya menerima `application/json`, memaksa preflight untuk request cross-origin |
| CORS | Deny-all |
| Session token | 32 byte acak dibuat saat startup, dikirim lewat URL pembuka, dikembalikan sebagai header `X-Session-Token`, diverifikasi konstan-waktu |
| Path | Client tidak pernah mengirim path filesystem; akses file lewat id di tabel `files` |
| URL input | Whitelist skema `http`/`https`; tolak `file://` dan sejenisnya sebelum mencapai yt-dlp |
| Eksekusi | `exec.Command` dengan argv eksplisit; tidak pernah `sh -c`; `--` sebelum URL |
| Config tool | `--ignore-config` agar `yt-dlp.conf` milik user tidak bisa menyuntikkan `--exec` |
| Headers | `X-Content-Type-Options: nosniff`, CSP ketat untuk SPA; satu-satunya host luar adalah `img-src https://i.ytimg.com` untuk sampul, dan font dilayani dari origin sendiri |
| Batas body | 64 KB, ditegakkan di middleware |

## 16. Tool acquisition dan versioning

Urutan discovery:

1. Tool terkelola di `<data_dir>/tools/`
2. Sidecar di direktori executable
3. `PATH` sistem

Bila tidak ada satu pun, app tetap berjalan dan `GET /api/health` melaporkan `TOOL_MISSING`; UI menawarkan unduh sekali klik. Aplikasi tidak boleh menolak start hanya karena tool belum ada.

### 16.1 Sumber binary

| Tool | Platform | Sumber | Bentuk |
| --- | --- | --- | --- |
| Tool | Platform | Sumber | Bentuk | Checksum |
| --- | --- | --- | --- | --- |
| yt-dlp | semua (`yt-dlp_macos` universal) | GitHub Releases resmi, per tag | binary tunggal | `SHA2-256SUMS` hulu, dicocokkan dengan digest aset GitHub |
| FFmpeg | windows/amd64 | `GyanD/codexffmpeg`, rilis per versi | satu `.zip` essentials berisi ffmpeg dan ffprobe | digest aset GitHub |
| FFmpeg | linux/amd64, linux/arm64, darwin/amd64, darwin/arm64 | `ffmpeg.martin-riedl.de`, direktori per versi rilis | `ffmpeg.zip` dan `ffprobe.zip` terpisah | berkas `.sha256` hulu |

Versi FFmpeg sama untuk seluruh platform dan mengikuti rilis GyanD. Versi yang belum tersedia di salah satu sumber menggagalkan pembaruan manifest, bukan menghasilkan manifest dengan versi campuran.

Setiap URL menunjuk rilis berversi tetap. Tag bergulir seperti `latest` dilarang dan dijaga oleh test manifest: isinya berganti, sehingga checksum yang di-pin terhadapnya pasti basi.

FFmpeg tidak punya distribusi binary statis resmi, jadi kedua sumber FFmpeg di atas adalah pihak ketiga. Itu keputusan rantai pasok, bukan detail implementasi, karena itu dicatat sebagai ADR-031. Konsekuensi hukumnya justru menguntungkan: karena binary diunduh di mesin pengguna dan tidak pernah ikut dalam artifact rilis, proyek ini tidak mendistribusikan ulang FFmpeg dan tidak memikul kewajiban LGPL/GPL.

### 16.2 Manifest

`internal/infrastructure/tools/manifest.json` di-commit dan di-embed. Skema 2: setiap build per platform berisi daftar `downloads` (URL, SHA-256, bentuk arsip, berkas yang diekstrak), karena sebagian sumber menerbitkan ffmpeg dan ffprobe sebagai arsip terpisah.

- **Fail-closed (ADR-033).** Build dengan satu saja unduhan tanpa `sha256` menolak instalasi dengan `TOOL_MANIFEST_INCOMPLETE`. Verifikasi tidak pernah dilewati, termasuk saat pengembangan.
- Seluruh unduhan sebuah build diverifikasi sebelum satu pun diekstrak, sehingga ffmpeg tidak pernah terpasang tanpa ffprobe akibat arsip kedua yang tidak cocok.
- Manifest dibuat oleh `make update-tools` (`go run ./scripts/toolmanifest`), bukan disunting manual. Program itu memakai checksum yang diterbitkan hulu dan menolak bila checksum hulu berbeda dengan digest GitHub. Opsi `-verify` mengunduh setiap berkas untuk mencocokkan checksum dan memastikan berkas yang akan diekstrak memang ada di dalam arsip. Hasilnya di-commit sebagai perubahan yang dapat direview.
- Test manifest menuntut seluruh platform rilis ter-pin, URL https tanpa tag bergulir, dan checksum 64 karakter heksadesimal.
- Menaikkan versi tool adalah commit tersendiri supaya regresi mudah di-bisect.

### 16.3 Instalasi

1. Unduh ke `<data_dir>/tmp/` dengan batas ukuran dan timeout.
2. Verifikasi SHA-256 terhadap manifest. Tidak cocok berarti berhenti dan berkas dibuang.
3. Ekstrak (`.zip` lewat stdlib, `.tar.xz` lewat ADR-032), ambil hanya berkas executable yang dibutuhkan.
4. Pasang ke `<data_dir>/tools/` lewat rename atomik.

Aturan lain:

- Pengecekan update mingguan, opsional (`tool_update_check`), dan tidak pernah memasang tanpa persetujuan user.
  - `application.ToolService` menanyakan tag rilis terbaru yt-dlp dan GyanD ke API GitHub saat startup bila sudah jatuh tempo, lalu memeriksa jatuh tempo setiap 6 jam. Hasil disimpan di `<data_dir>/tool-updates.json`; offline tidak memajukan waktu cek maupun menghapus hasil lama.
  - Versi dibandingkan per bagian angka. Versi yang tidak bisa diurai, seperti snapshot `N-…`, tidak pernah ditandai tertinggal.
  - `POST /api/tools/check` menjalankan cek sekarang. `POST /api/tools/update` hanya menerima `yt-dlp` (pengecualian ADR-033): tag divalidasi dengan pola tanggal sebelum menyusun URL, checksum diambil dari `SHA2-256SUMS` rilis tersebut, lalu unduhan diverifikasi dan dipasang lewat jalur yang sama dengan instalasi manifest.
  - UI menandai tool yang tertinggal. yt-dlp mendapat tombol **Perbarui**; FFmpeg hanya diberi petunjuk (rilis aplikasi untuk tool terkelola, package manager untuk tool dari `PATH`).
- `GET /api/health` menampilkan versi aktual hasil `yt-dlp --version` / `ffmpeg -version` agar bug report dapat dikaitkan ke versi tool.
- Tool dari `PATH` dipakai apa adanya tanpa verifikasi checksum: itu milik sistem pengguna, bukan sesuatu yang kita pasang.

## 17. Storage layout dan konfigurasi

| OS | Data dir | Default output |
| --- | --- | --- |
| Windows | `%LOCALAPPDATA%\yt-to-mp3\` | `%USERPROFILE%\Music\yt-to-mp3\` |
| macOS | `~/Library/Application Support/yt-to-mp3/` | `~/Music/yt-to-mp3/` |
| Linux | `$XDG_DATA_HOME/yt-to-mp3/` (fallback `~/.local/share/`) | `$XDG_MUSIC_DIR` atau `~/Music/yt-to-mp3/` |

Isi data dir: `db/app.db`, `tools/`, `tmp/`, `logs/`, `config.json`, `app.lock`, `runtime.json`.

Presedensi konfigurasi: default built-in → `config.json` → env var → tabel `settings`.

Kunci: `output_dir`, `max_concurrent_jobs` (default **2**), `max_queue_depth` (50), `default_preset_id`, `filename_mode`, `tool_update_check`, `idle_shutdown_minutes` (30), `log_level`.

Concurrency default sengaja rendah: lebih dari 2–3 unduhan paralel dari satu IP memicu throttling dan HTTP 429.

Koneksi SQLite: `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`, dengan **pool baca terpisah dari satu koneksi tulis** (ADR-024).

## 18. Siklus hidup aplikasi

Ringkasan; urutan lengkap ada di [architecture.md §7–§9](architecture.md).

- **Single instance** dijaga lock file. Launch kedua membaca `runtime.json`, membuka browser ke instance yang sudah jalan, lalu keluar (ADR-022).
- **Cara keluar** ada tiga: tombol Quit di SPA (`POST /api/shutdown`), idle shutdown setelah 30 menit tanpa job dan tanpa koneksi, dan sinyal OS. Tidak ada tray icon (ADR-021).
- **Crash recovery** berjalan sebelum listener dibuka: job non-terminal jadi `INTERRUPTED`, temp yatim dibersihkan, baris `files` yang filenya hilang ditandai.
- **Housekeeping** saat startup lalu per jam: pangkas `job_events`, GC temp, kedaluwarsa cache metadata, rekonsiliasi `files.missing` dua arah. Rotasi log dikerjakan writer log saat hari berganti.
- **Log** ditulis ke `<data_dir>/logs/app.log` dan terminal. URL bertoken hanya dicetak ke terminal, karena berkas log lazim dilampirkan pada laporan bug.

## 19. Klasifikasi error dan kebijakan retry

| Kelas | Contoh | Kebijakan |
| --- | --- | --- |
| Transient | timeout, connection reset, DNS gagal, 5xx, interupsi | auto-retry maks 3×, backoff 2s/8s/30s |
| Throttled | HTTP 429 | auto-retry maks 3×, backoff lebih panjang + jitter, turunkan concurrency sementara |
| Tool outdated | ekstraksi signature gagal, format tidak ditemukan | tidak auto-retry; `TOOL_OUTDATED` dengan tombol **Perbarui yt-dlp & coba lagi** di notifikasi dan riwayat, serta **Perbarui yt-dlp** saat analisis gagal |
| Permanen | private, dihapus, geo-block, age-gate, livestream, 404 | tidak pernah retry; tombol retry di UI dinonaktifkan |
| Lokal | disk penuh, permission denied, path invalid | tidak retry; pesan actionable |

Retry bersifat idempoten (ADR-008): selalu menulis ke path temp baru dan hanya menyentuh output final pada langkah commit.

Implementasi:

- Auto-retry memakai baris job yang sama, bukan job baru. Scheduler mengembalikan job ke `queued` (satu-satunya transisi mundur yang diizinkan state machine, dari status aktif mana pun kecuali `cancelling`), menaikkan `attempt_count`, menyimpan kode kegagalan terakhir, dan mengisi `retry_at`. `ClaimNextQueued` melewati job sampai `retry_at` lewat, lalu membersihkan kode error saat mengambilnya.
- Jeda disimpan di database, jadi tetap berlaku bila aplikasi dibuka ulang di tengah jeda. Jeda throttled 30 detik, 2 menit, dan 5 menit, masing-masing diacak ±20%.
- Setelah 429, scheduler hanya menjalankan satu job selama 5 menit.
- Retry manual dari UI membuat job baru dengan `attempt_count` 0, yaitu jatah auto-retry penuh.
- Selama menunggu jeda, UI menampilkan hitung mundur, nomor percobaan, dan alasan kegagalan terakhir.

## 20. Normalisasi URL dan `source_key`

- Ekstrak video id dari `watch?v=ID`, `youtu.be/ID`, `shorts/ID`, `embed/ID`
- Buang query lain (`list`, `t`, `si`, `feature`, parameter tracking)
- Normalisasi host dan skema; buang `www.`
- Hasil: `youtube:<ID>`; sumber lain memakai prefix penyedianya
- URL tak dikenali ditolak `UNSUPPORTED_URL` sebelum memanggil yt-dlp

## 21. Sanitasi filename

Target paling ketat (Windows) berlaku untuk semua platform:

- Ganti karakter ilegal `< > : " / \ | ? *` dan kontrol `0x00`–`0x1F` dengan `_`
- Tolak/rename nama reserved: `CON`, `PRN`, `AUX`, `NUL`, `COM1`–`COM9`, `LPT1`–`LPT9`, termasuk yang berekstensi
- Buang titik dan spasi di akhir nama
- Normalisasi Unicode ke NFC; pertahankan non-ASCII dan emoji, potong pada batas rune
- Batasi total path 240 karakter (margin dari limit 260 Windows); potong judul, pertahankan ekstensi
- Nama kosong setelah sanitasi jatuh ke `source_key`
- Tabrakan diselesaikan dengan sufiks ` (2)`, ` (3)`, dan seterusnya, dimenangkan lewat reservasi atomik (ADR-029)

## 22. Non-functional requirements

Tanpa angka, tidak ada dasar untuk menyebut sesuatu regresi:

| Aspek                                             | Target      |
| ------------------------------------------------- | ----------- |
| Ukuran binary (tanpa tool)                        | < 25 MB     |
| Cold start sampai SPA tampil                      | < 1.5 detik |
| Memori idle                                       | < 60 MB     |
| Memori saat 2 job berjalan                        | < 200 MB    |
| CPU aplikasi sendiri, di luar tool                | < 5%        |
| Latensi API non-download, p95                     | < 50 ms     |
| Konversi lagu 5 menit @192 kbps, jaringan 20 Mbps | < 45 detik  |
| Kapasitas history tanpa degradasi terasa          | 10.000 job  |

### 22.1 Hasil pengukuran

Diukur dengan `make nfr` (`go run ./scripts/nfr`) pada 2026-09-13, Windows 11 amd64, 16 CPU, Go 1.27, binary rilis (`-trimpath -ldflags "-s -w"`). Setiap pengukuran berjalan di direktori data sementara; riwayat 10.000 job disemai langsung ke SQLite sebagai job terminal.

| Aspek | Target | Hasil |
| --- | --- | --- |
| Ukuran binary | < 25 MB | 13,5 MB |
| Cold start sampai SPA tersaji (median 5×) | < 1,5 detik | 97 ms |
| Memori idle (working set) | < 60 MB | 19,3 MB |
| Latensi API non-download, p95 terburuk (`/api/health`) | < 50 ms | 2,3 ms |
| Daftar riwayat (`status=finished`, jalur yang dipakai UI), 10.000 job, p95 | tanpa degradasi terasa | 1,0 ms |
| Memori puncak proses aplikasi saat 2 job berjalan | < 200 MB | 30,8 MB |
| CPU proses aplikasi di luar tool, rata-rata per satu core | < 5% | 1,3% |
| Konversi lagu 3 menit 33 detik, dua preset paralel, jaringan rumah | < 45 detik untuk 5 menit | 15,9 detik |

Tiga baris terakhir berasal dari `make nfr URL="<tautan>"`, yang butuh jaringan dan yt-dlp serta FFmpeg di `PATH`. Waktu konversi bergantung pada jaringan, jadi dicatat sebagai catatan, bukan lolos/gagal.

Pengukuran pertama menemukan satu regresi: daftar riwayat berfilter status butuh p95 38 ms pada 10.000 job, karena indeks `status` saja memaksa SQLite mengurutkan seluruh baris di memori. Migrasi `00002` menggantinya dengan indeks komposit `(status, created_at DESC, id DESC)`, dan test memeriksa rencana query-nya supaya regresi yang sama tertangkap walau tidak terlihat pada database kecil. Migrasi `00003` menambahkan indeks `(created_at DESC, id DESC)` untuk riwayat tanpa filter status: p95 daftar tanpa filter turun dari 2,1 ms menjadi 1,0 ms.

## 23. Strategi testing

### Unit

preset resolver; state transition; filename sanitizer (nama reserved, pemotongan Unicode, batas panjang); progress parser yt-dlp dan ffmpeg termasuk kasus total tidak diketahui; error mapping stderr → `code`; normalisasi URL → `source_key`; pagination; klasifikasi retryable; perhitungan timeout dari durasi; estimasi preflight disk.

### Integration

migrasi di database kosong dan di snapshot versi sebelumnya; repositories; fake process runner; timeout/cancel/cleanup; verifikasi output; crash recovery sweep; reservasi nama file di bawah kontensi dua worker; penjagaan `schema_version`.

### Contract/API

status code; JSON golden files; skema dan urutan SSE, heartbeat, resume `Last-Event-ID`, subscriber terlambat; middleware keamanan dengan Host salah, Origin salah, token hilang, body melebihi batas; kelengkapan terjemahan untuk setiap `error.code`.

### E2E

Media fixture yang haknya jelas atau fixture sintetis. Jangan menjadikan URL publik sebagai satu-satunya test oracle.

Implementasi (`internal/e2e`, tag `integration`): binary test berperan sebagai yt-dlp palsu yang dikendalikan variabel lingkungan, sedangkan JobService, scheduler, pipeline, SQLite, store berkas, dan FFmpeg sungguhan. Skenario yang dicakup:

- konversi lengkap, termasuk judul dari metadata, tag, dan pembersihan temp
- auto-retry setelah kegagalan jaringan sementara
- pembatalan saat mengunduh yang harus menghentikan proses dan tidak meninggalkan berkas

Kontrak API dikunci berkas golden (`internal/api/testdata/golden/`) untuk daftar job, satu job, error, metadata, tool, preset, dan health, dengan pemeriksaan bahwa path filesystem tidak pernah bocor.

### Cross-platform

Smoke test untuk Windows/Linux/macOS dan seluruh target arsitektur rilis. Khusus Windows, wajib ada test yang memverifikasi seluruh process tree mati setelah cancel dan file temp benar-benar terhapus. Tambahan: test single-instance, yakni launch kedua tidak membuka server baru.

## 24. Rencana frontend

Layar:

Satu halaman kerja dengan top bar (status tool, **Setelan**, **Keluar**) dan status bar (folder keluaran, ringkasan antrean):

1. **Tangkap** — tempel URL (analisis otomatis saat paste), preview sampul dan metadata, pilih preset, mulai job
2. **Sedang diproses** — job aktif; sampul abu-abu terisi warna dari bawah mengikuti progress, cancel
3. **Selesai** — unduh, buka folder, retry, hapus dengan konfirmasi inline
4. **Setelan** — dialog modal (`Ctrl+,`) dengan grup Penyimpanan (pemilih folder native), Konversi, Antrean, Tool, Tampilan (bahasa, tema), Lanjutan, Tentang

Teknis:

- Server state lewat TanStack Query; state lokal UI dengan `useState`/`useReducer`, tanpa global store
- Satu hook `useJobStream(jobId)` membungkus `EventSource`, menangani reconnect dan `Last-Event-ID`
- Progress dirender dari state SSE, dengan polling `GET /api/jobs/:id` sebagai fallback bila stream gagal
- Preset dan opsi berasal dari `GET /api/presets` dan `GET /api/settings`
- Teks error diterjemahkan dari `error.code` (ADR-027); tidak pernah menampilkan `message` apa adanya

## 25. Build, dev workflow, dan rilis

- **Chicken-and-egg `embed`**: `//go:embed web/dist` gagal compile bila folder belum ada, sehingga clone baru dan CI langsung merah. Commit `web/dist/.gitkeep` + `index.html` placeholder, dan pisahkan dua mode dengan build tag — `dev` memakai `os.DirFS` atau proxy ke Vite, `prod` memakai `embed.FS`.
- `make dev` menjalankan Vite dan Go bersamaan; `make build` menjalankan `vite build` sebelum `go build`.
- Matriks CI: `windows/amd64`, `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
- Rilis dengan GoReleaser (`.goreleaser.yaml`, workflow `release.yml`), artifact `yt-to-mp3_<version>_<os>_<arch>` (zip untuk Windows, tar.gz lainnya), disertai `checksums.txt` SHA-256. Hook `before` membangun SPA dan menjalankan `check:i18n` sebelum `go build`, karena SPA disematkan. Tag `v*` membuat rilis **draft**; pemicu manual menjalankan snapshot tanpa menerbitkan apa pun. windows/arm64 sengaja tidak dibangun karena tidak punya entri manifest tool.
- **Signing**: macOS perlu codesign + notarization, kalau tidak Gatekeeper memblokir. Di Windows, binary Go tanpa signature yang men-spawn subprocess sering kena false positive SmartScreen/AV — anggarkan sertifikat atau dokumentasikan langkah bypass.
- Logging `log/slog` terstruktur ke `<data_dir>/logs/app.log`, rotasi harian, retensi 7 hari.

## 26. Roadmap

| # | Tahap | Isi |
| --- | --- | --- |
| 1 | Foundation | Skeleton layer, embed SPA, health, middleware keamanan (§15), single instance + lifecycle (§18) |
| 2 | Tooling | Discovery, acquisition, checksum (§16), endpoint metadata |
| 3 | Persistence | SQLite, migrasi, seed preset, repository, crash recovery |
| 4 | Job engine | Scheduler, worker pool, state machine, SSE hub |
| 5 | Pipeline | yt-dlp + ffmpeg, progress dua fase, process tree kill, preflight |
| 6 | Output | Tagging, verifikasi, commit atomik, files/history/cancel/retry |
| 7 | Frontend | Empat layar, i18n, settings |
| 8 | Packaging | Cross-platform, signing, GoReleaser |
| 9 | Hardening | Regression, NFR, housekeeping |

## 27. Acceptance criteria MVP

- executable lokal menyajikan SPA
- analyze tidak otomatis download
- livestream ditolak sebelum ada byte yang diunduh
- job asynchronous, preset MP3 utama berfungsi
- progress dapat dipantau, termasuk saat ukuran total tidak diketahui
- cancel membersihkan **seluruh process tree** dan temp, terverifikasi di Windows
- history bertahan setelah restart, job terinterupsi dipulihkan ke state terminal
- launch kedua tidak membuat instance baru
- aplikasi bisa dihentikan tanpa Task Manager
- tidak ada arbitrary shell execution
- server hanya listen di loopback dan menolak Host/Origin asing
- output MP3 punya ID3 tag dan cover art
- tool hilang tidak membuat app gagal start, dan dilaporkan lewat health
- setiap `error.code` punya terjemahan di SPA
- target NFR §22 terpenuhi pada mesin referensi
- CI punya unit + integration + smoke build untuk seluruh matriks
- perubahan versi yt-dlp/FFmpeg memicu regression test

## 28. Catatan operasional dan legal

Mengunduh konten YouTube umumnya melanggar Terms of Service platform tersebut, dan hak cipta atas materi yang diunduh tetap pada pemiliknya. Aplikasi ini ditargetkan sebagai alat lokal-personal, tanpa distribusi, sharing, atau hosting konten. Konsekuensi teknisnya: yt-dlp akan rusak secara berkala mengikuti perubahan sisi YouTube — itulah alasan ADR-013 memilih tool yang dapat di-update terpisah dari siklus rilis aplikasi, dan alasan regression test diikatkan pada versi tool.

## 29. Referensi

- Go embed: https://pkg.go.dev/embed
- FFmpeg codecs / libmp3lame: https://www.ffmpeg.org/ffmpeg-codecs.html
- FFmpeg progress reporting: https://ffmpeg.org/ffmpeg.html
- yt-dlp: https://github.com/yt-dlp/yt-dlp
- yt-dlp output/progress template: https://github.com/yt-dlp/yt-dlp#output-template
- modernc.org/sqlite: https://pkg.go.dev/modernc.org/sqlite
- SQLite WAL: https://www.sqlite.org/wal.html
- goose: https://github.com/pressly/goose
- Windows Job Objects: https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects
- Windows naming files and paths: https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file
- GoReleaser: https://goreleaser.com/

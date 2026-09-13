# Data Model

Skema SQLite, invarian, dan kebijakan retensi. Konteks arsitektur ada di [architecture.md](architecture.md); perencanaan produk di [yt-to-mp3-go-planning.md](yt-to-mp3-go-planning.md).

## 1. Konvensi

- Semua timestamp `TEXT` ISO-8601 **UTC** (`2026-09-12T09:00:00Z`). Konversi ke waktu lokal hanya terjadi saat render di UI.
- Semua id aplikasi berupa `TEXT` dengan prefix tipe (`job_`, `file_`) agar id yang tertukar langsung terlihat di log.
- Boolean disimpan sebagai `INTEGER` 0/1.
- Kolom yang bisa tidak diketahui bernilai `NULL`, bukan sentinel seperti `-1` atau string kosong.

Pragma koneksi:

```text
journal_mode = WAL
busy_timeout = 5000
foreign_keys = ON
synchronous  = NORMAL
```

Dua handle terpisah: pool baca dengan beberapa koneksi, dan **satu** koneksi tulis. WAL mengizinkan banyak pembaca berbarengan dengan satu penulis, jadi membatasi seluruh pool ke satu koneksi akan menyerialkan pembacaan tanpa perlu.

## 2. Tabel

### 2.1 `jobs`

```sql
CREATE TABLE jobs (
  id             TEXT PRIMARY KEY,
  source_url     TEXT NOT NULL,
  source_key     TEXT NOT NULL,
  title          TEXT,
  status         TEXT NOT NULL CHECK (status IN (
                   'queued','resolving','downloading','converting',
                   'verifying','completed','failed','cancelling','cancelled')),
  preset_id      TEXT NOT NULL REFERENCES presets(id),
  filename_mode  TEXT NOT NULL DEFAULT 'title'
                   CHECK (filename_mode IN ('title','title-uploader','uploader-title','id')),
  progress       REAL CHECK (progress IS NULL OR (progress >= 0 AND progress <= 100)),
  phase          TEXT,
  attempt_count  INTEGER NOT NULL DEFAULT 0,
  error_code     TEXT,
  error_message  TEXT,
  created_at     TEXT NOT NULL,
  started_at     TEXT,
  finished_at    TEXT
);

-- Migrasi 00002: riwayat berfilter status dilayani indeks tanpa sort di memori.
CREATE INDEX idx_jobs_status_created ON jobs(status, created_at DESC, id DESC);
CREATE INDEX idx_jobs_created ON jobs(created_at DESC);

-- Menegakkan DUPLICATE_ACTIVE_JOB di level database, bukan hanya di kode.
CREATE UNIQUE INDEX idx_jobs_active_dedup
  ON jobs(source_key, preset_id)
  WHERE status NOT IN ('completed','failed','cancelled');
```

`progress` bernilai `NULL` berarti indeterminate — ukuran total atau durasi tidak diketahui. Ini nilai yang sah, bukan data hilang.

### 2.2 `media_items`

Cache metadata source, dikunci `source_key` hasil normalisasi URL.

```sql
CREATE TABLE media_items (
  source_key    TEXT PRIMARY KEY,
  title         TEXT NOT NULL,
  uploader      TEXT,
  duration_ms   INTEGER,
  thumbnail_url TEXT,
  source_codec  TEXT,
  is_live       INTEGER NOT NULL DEFAULT 0,
  raw_json      TEXT,
  fetched_at    TEXT NOT NULL
);

CREATE INDEX idx_media_fetched ON media_items(fetched_at);
```

`raw_json` menyimpan keluaran mentah yt-dlp untuk diagnosis ketika pemetaan field berubah setelah update tool. Tidak pernah dikirim ke UI.

### 2.3 `files`

```sql
CREATE TABLE files (
  id         TEXT PRIMARY KEY,
  job_id     TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  path       TEXT NOT NULL,
  filename   TEXT NOT NULL,
  mime       TEXT NOT NULL,
  size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
  sha256     TEXT NOT NULL,
  missing    INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);

CREATE UNIQUE INDEX idx_files_job ON files(job_id);
```

Satu job menghasilkan tepat satu file. `path` tidak pernah keluar lewat API; klien hanya memakai `id`.

### 2.4 `job_events`

```sql
CREATE TABLE job_events (
  job_id     TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  seq        INTEGER NOT NULL,
  type       TEXT NOT NULL CHECK (type IN ('state','error','done')),
  payload    TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (job_id, seq)
);
```

`CHECK` sengaja **tidak** memuat `progress`. Event progress hidup di memori saja; mempersistkannya pada 4 event/detik menghasilkan ribuan baris per job dan memperlambat query replay SSE. Nomor `seq` tetap diambil dari counter yang sama agar `Last-Event-ID` konsisten.

### 2.5 `presets`

```sql
CREATE TABLE presets (
  id           TEXT PRIMARY KEY,
  label        TEXT NOT NULL,
  format       TEXT NOT NULL,
  codec        TEXT NOT NULL,
  mode         TEXT NOT NULL CHECK (mode IN ('cbr','vbr')),
  bitrate_kbps INTEGER,
  vbr_quality  INTEGER,
  sample_rate  INTEGER,  -- NULL = ikuti sumber, hanya untuk preset lossless
  channels     INTEGER NOT NULL,
  extra_args   TEXT NOT NULL DEFAULT '[]',
  sort_order   INTEGER NOT NULL,
  deprecated   INTEGER NOT NULL DEFAULT 0,
  CHECK ((mode = 'cbr' AND bitrate_kbps IS NOT NULL)
      OR (mode = 'vbr' AND vbr_quality  IS NOT NULL))
);
```

Seed MVP:

| id             | label    | mode | parameter                    |
| -------------- | -------- | ---- | ---------------------------- |
| `mp3_economy`  | Economy  | cbr  | 128 kbps                     |
| `mp3_standard` | Standard | cbr  | 192 kbps                     |
| `mp3_high`     | High     | cbr  | 256 kbps                     |
| `mp3_max`      | Max      | cbr  | 320 kbps                     |
| `mp3_vbr_v0`   | VBR High | vbr  | `vbr_quality = 0` (`-q:a 0`) |

Semua seed memakai `sample_rate = 48000`, `channels = 2`.

48 kHz dipilih agar cocok dengan sumber, bukan sekadar mengikuti standar CD (ADR-030). `bestaudio` YouTube hampir selalu Opus, yang secara desain hanya beroperasi di 48 kHz, sehingga 44.1 kHz akan memaksa resampling pada mayoritas unduhan. Sumber AAC 44.1 kHz yang lebih jarang akan ter-upsample, dan itu tidak merugikan.

`preset_id` tersimpan permanen di `jobs`, sehingga nilainya adalah kontrak selamanya: tabel ini **append-only**. Preset yang tidak lagi ditawarkan ditandai `deprecated = 1` agar tetap tersembunyi di UI tanpa membuat history lama jadi yatim. Kolom `vbr_quality` adalah satu-satunya sumber kebenaran untuk V0 — jangan menuliskan angka itu lagi di tempat lain.

### 2.6 `settings`

```sql
CREATE TABLE settings (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  type       TEXT NOT NULL CHECK (type IN ('string','int','bool')),
  updated_at TEXT NOT NULL
);
```

Hanya memuat nilai yang benar-benar diubah user. Kunci yang tidak ada berarti memakai default dari konfigurasi.

### 2.7 `schema_meta`

```sql
CREATE TABLE schema_meta (
  id             INTEGER PRIMARY KEY CHECK (id = 1),
  app_version    TEXT NOT NULL,
  schema_version INTEGER NOT NULL
);
```

Saat startup, aplikasi menolak berjalan bila `schema_version` di database lebih tinggi daripada yang dikenalnya, dengan pesan jelas bahwa versi aplikasi lebih lama daripada datanya. Tanpa penjagaan ini, rollback ke versi lama akan menabrak skema yang sudah dimigrasi dan gagal dengan cara yang membingungkan.

## 3. Invarian

Ditegakkan oleh skema jika memungkinkan, sisanya oleh test integrasi:

1. Job non-terminal unik per `(source_key, preset_id)` — index parsial.
2. `job_events` tidak pernah memuat baris bertipe `progress` — CHECK.
3. `(job_id, seq)` unik dan `seq` naik monoton per job — primary key.
4. Setiap job `completed` punya tepat satu baris `files` — index unik + test.
5. `finished_at` terisi jika dan hanya jika status terminal — test.
6. `preset_id` pada job lama selalu masih bisa di-resolve — dijamin sifat append-only.
7. Menghapus job ikut menghapus `files` dan `job_events` — `ON DELETE CASCADE`.
8. Transisi status dan event yang menyertainya ditulis dalam satu transaksi — test.

## 4. Retensi

| Data | Kebijakan |
| --- | --- |
| `jobs` | Disimpan sampai user menghapus |
| `job_events` | Dipangkas untuk job terminal yang `finished_at`-nya lebih tua dari 30 hari; baris `jobs` tidak disentuh |
| `media_items` | TTL 24 jam untuk pemakaian sebagai cache; baris kedaluwarsa dan tak dirujuk dibuang housekeeper |
| `files` | Baris tetap ada walau file hilang di disk, ditandai `missing = 1`. Penanda disamakan dengan disk saat startup dan setiap jam, dua arah: file yang muncul kembali mendapat `missing = 0`. Hanya "tidak ada" yang menandai hilang; error lain seperti izin ditolak membiarkan penanda |

Seluruh kebijakan di atas dijalankan `application.Housekeeper`: satu putaran sebelum scheduler dan listener dimulai, lalu setiap jam. Setiap tugas berdiri sendiri, jadi kegagalan satu tugas dicatat tanpa menghentikan yang lain.

## 5. Migrasi

- Dikelola `goose` dengan file SQL yang di-embed lewat `embed.FS`.
- Maju saja. Tidak ada rollback otomatis di runtime; penurunan versi ditangani oleh penjagaan `schema_version`.
- Seed `presets` adalah bagian dari migrasi, bukan kode aplikasi, supaya database baru dan lama sampai ke keadaan yang sama.
- Setiap migrasi wajib punya test yang menjalankannya di atas database kosong dan di atas snapshot versi sebelumnya.

| Versi | Berkas | Isi |
| --- | --- | --- |
| 1 | `00001_init.sql` | Skema awal dan seed preset |
| 2 | `00002_jobs_status_created_index.sql` | Indeks `(status, created_at DESC, id DESC)` menggantikan `idx_jobs_status`. Diukur: p95 daftar riwayat berfilter status pada 10.000 job turun dari 38 ms menjadi 1,7 ms |

`TestMigrateDariSkemaVersi1` membangun database lewat `goose UpTo(1)`, mengisinya dengan SQL mentah, lalu menjalankan `Migrate` penuh dan memeriksa data tetap utuh. `TestRiwayatBerfilterStatusMemakaiIndeks` memeriksa `EXPLAIN QUERY PLAN` supaya regresi indeks tertangkap walau tidak terlihat pada database kecil.

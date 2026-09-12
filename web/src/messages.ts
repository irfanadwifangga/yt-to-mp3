import { ApiError } from "./api";

/**
 * Teks dirakit di klien dari `error.code`, bukan dari field `message`.
 *
 * Field message pada respons hanya fallback untuk developer; menjadikannya
 * sumber teks UI berarti ada dua tempat yang menentukan kalimat yang sama.
 * Lihat ADR-027.
 */
const MESSAGES: Record<string, string> = {
  // Masukan
  INVALID_URL: "URL tidak valid.",
  INVALID_SETTING: "Nilai setelan tidak valid.",
  UNSUPPORTED_URL: "URL ini bukan tautan video YouTube.",
  BAD_REQUEST: "Permintaan tidak dapat dibaca.",
  UNSUPPORTED_MEDIA_TYPE: "Format permintaan tidak didukung.",

  // Sumber
  LIVE_NOT_SUPPORTED: "Siaran langsung tidak didukung.",
  VIDEO_PRIVATE: "Video bersifat privat.",
  VIDEO_UNAVAILABLE: "Video tidak tersedia.",
  GEO_BLOCKED: "Video diblokir di wilayah ini.",
  AGE_RESTRICTED: "Video dibatasi usia dan tidak dapat diproses.",
  RATE_LIMITED: "Terlalu banyak permintaan. Coba lagi beberapa saat lagi.",

  // Tool
  TOOL_MISSING: "Tool yang dibutuhkan belum terpasang.",
  TOOL_OUTDATED: "yt-dlp perlu diperbarui.",
  TOOL_MANIFEST_INCOMPLETE: "Versi tool belum di-pin di manifest, jadi instalasi otomatis ditolak.",
  TOOL_CHECKSUM_MISMATCH: "Checksum unduhan tidak cocok. Instalasi dibatalkan.",
  TOOL_INSTALL_FAILED: "Instalasi tool gagal.",

  // Pipeline
  DOWNLOAD_FAILED: "Unduhan gagal.",
  TRANSCODE_FAILED: "Konversi gagal.",
  VERIFY_FAILED: "Hasil konversi tidak lolos pemeriksaan.",
  DISK_FULL: "Ruang disk tidak mencukupi.",
  OUTPUT_WRITE_FAILED: "Berkas hasil tidak dapat ditulis.",
  TIMEOUT: "Proses melewati batas waktu.",
  INTERRUPTED: "Terhenti karena aplikasi ditutup.",
  CANCELLED: "Dibatalkan.",

  // Antrean
  QUEUE_FULL: "Antrean penuh. Tunggu beberapa job selesai.",
  DUPLICATE_ACTIVE_JOB: "Video ini sudah ada di antrean.",
  JOB_NOT_FOUND: "Job tidak ditemukan.",

  // Transport
  UNAUTHORIZED: "Session token tidak valid. Buka ulang aplikasi dari shortcut.",
  FORBIDDEN_HOST: "Host tidak diizinkan.",
  FORBIDDEN_ORIGIN: "Origin tidak diizinkan.",
  NOT_FOUND: "Tidak ditemukan.",
  INTERNAL: "Terjadi kesalahan internal."
};

/** Menerjemahkan error jadi kalimat untuk pengguna. */
export function messageFor(err: unknown, fallback = "Terjadi kesalahan"): string {
  if (err instanceof ApiError) {
    return MESSAGES[err.code] ?? `${fallback} (${err.code}).`;
  }
  return "Tidak dapat menghubungi server.";
}

/** Menerjemahkan kode error yang tersimpan pada job. */
export function messageForCode(code?: string): string {
  if (!code) return "";
  return MESSAGES[code] ?? code;
}

const PHASES: Record<string, string> = {
  resolving: "Membaca metadata",
  downloading: "Mengunduh",
  converting: "Mengonversi",
  verifying: "Memeriksa"
};

export function phaseLabel(phase?: string): string {
  if (!phase) return "";
  return PHASES[phase] ?? phase;
}

const STATUSES: Record<string, string> = {
  queued: "Antre",
  resolving: "Membaca metadata",
  downloading: "Mengunduh",
  converting: "Mengonversi",
  verifying: "Memeriksa",
  completed: "Selesai",
  failed: "Gagal",
  cancelling: "Membatalkan",
  cancelled: "Dibatalkan"
};

export function statusLabel(status: string): string {
  return STATUSES[status] ?? status;
}

export function formatDuration(ms: number): string {
  if (ms <= 0) return "tidak diketahui";
  const total = Math.round(ms / 1000);
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

export function formatBytes(bytes: number): string {
  if (bytes <= 0) return "-";
  const mb = bytes / (1024 * 1024);
  return mb >= 1 ? `${mb.toFixed(1)} MB` : `${(bytes / 1024).toFixed(0)} KB`;
}

export function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  // Timestamp disimpan UTC dan baru dilokalkan di sini.
  return d.toLocaleString();
}

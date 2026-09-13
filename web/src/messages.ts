import { ApiError } from "./api";
import { dateLocale, has, t } from "./i18n";

/**
 * Teks dirakit di klien dari `error.code`, bukan dari field `message`.
 *
 * Field message pada respons hanya fallback untuk developer; menjadikannya
 * sumber teks UI berarti ada dua tempat yang menentukan kalimat yang sama.
 * Lihat ADR-027.
 */
export function messageFor(err: unknown, context = t("common.error")): string {
  if (err instanceof ApiError) {
    const key = `error.${err.code}`;
    return has(key) ? t(key) : t("common.errorWithCode", { context, code: err.code });
  }
  return t("common.network");
}

/** Menerjemahkan kode error yang tersimpan pada job. */
export function messageForCode(code?: string): string {
  if (!code) return "";
  const key = `error.${code}`;
  return has(key) ? t(key) : code;
}

export function phaseLabel(phase?: string): string {
  if (!phase) return "";
  const key = `phase.${phase}`;
  return has(key) ? t(key) : phase;
}

export function statusLabel(status: string): string {
  const key = `status.${status}`;
  return has(key) ? t(key) : status;
}

export function formatDuration(ms: number): string {
  if (ms <= 0) return t("format.unknownDuration");
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
  // Timestamp disimpan UTC dan baru dilokalkan di sini, mengikuti bahasa UI.
  return d.toLocaleString(dateLocale);
}

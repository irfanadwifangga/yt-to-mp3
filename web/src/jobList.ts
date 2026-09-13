import type { Job } from "./api";

/**
 * Menggabungkan dua daftar job tanpa duplikat, terbaru di atas.
 *
 * Baris dari `fresh` menang karena statusnya lebih baru. Tanggal dibanding
 * sebagai waktu, bukan string: pecahan detik RFC 3339 dari server panjangnya
 * tidak tetap.
 */
export function mergeJobs(fresh: Job[], previous: Job[]): Job[] {
  const byId = new Map<string, Job>();
  for (const job of previous) byId.set(job.id, job);
  for (const job of fresh) byId.set(job.id, job);
  return [...byId.values()].sort(
    (a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) || b.id.localeCompare(a.id)
  );
}

/**
 * Job di halaman riwayat yang sebelumnya terlihat aktif dan kini tidak lagi,
 * yaitu yang baru saja selesai, gagal, atau dibatalkan dan perlu diumumkan.
 */
export function newlyFinished(previous: Set<string>, finished: Job[], nowActive: Set<string>): Job[] {
  return finished.filter((job) => previous.has(job.id) && !nowActive.has(job.id));
}

/**
 * Job yang sebelumnya aktif, kini tidak aktif, tetapi belum muncul di
 * halaman riwayat yang diambil. Tetap dilacak supaya selesainya diumumkan
 * pada polling berikutnya, bukan hilang tanpa notifikasi karena kedua
 * daftar diambil pada saat yang sedikit berbeda.
 */
export function pendingIds(previous: Set<string>, finished: Job[], nowActive: Set<string>): string[] {
  const seen = new Set(finished.map((job) => job.id));
  return [...previous].filter((id) => !nowActive.has(id) && !seen.has(id));
}

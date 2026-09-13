import en from "./locales/en.json";
import id from "./locales/id.json";

export type Locale = "id" | "en";

const DICTIONARIES: Record<Locale, Record<string, string>> = { id, en };
const STORAGE_KEY = "yt2mp3.locale";

/**
 * Bahasa dipilih dari pilihan tersimpan, lalu bahasa browser.
 *
 * Inggris menjadi cadangan untuk semua bahasa selain Indonesia: pengguna
 * yang browsernya berbahasa lain lebih mungkin membaca Inggris daripada
 * Indonesia. Lihat docs planning "Kode error dan lokalisasi".
 */
function detect(): Locale {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "id" || stored === "en") return stored;
  } catch {
    // Penyimpanan bisa diblokir browser; jatuh ke deteksi bahasa.
  }
  const lang = (navigator.languages?.[0] ?? navigator.language ?? "").toLowerCase();
  return lang.startsWith("id") ? "id" : "en";
}

export const locale: Locale = detect();
export const dateLocale = locale === "id" ? "id-ID" : "en-US";

document.documentElement.lang = locale;

/** Melaporkan apakah sebuah kunci punya terjemahan. */
export function has(key: string): boolean {
  return key in DICTIONARIES[locale] || key in DICTIONARIES.en;
}

/**
 * Menerjemahkan kunci, dengan placeholder {nama} diisi dari vars.
 *
 * Kunci yang hilang di bahasa aktif jatuh ke Inggris, lalu ke kuncinya
 * sendiri, sehingga teks yang belum diterjemahkan terlihat jelas alih-alih
 * menghilang tanpa jejak.
 */
export function t(key: string, vars?: Record<string, string | number>): string {
  const text = DICTIONARIES[locale][key] ?? DICTIONARIES.en[key] ?? key;
  if (!vars) return text;
  return text.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in vars ? String(vars[name]) : match,
  );
}

/**
 * Mengganti bahasa lalu memuat ulang halaman.
 *
 * Memuat ulang lebih sederhana dan lebih pasti daripada merender ulang
 * seluruh pohon: tidak ada komponen yang tertinggal memegang teks lama.
 * Session token aman karena tersimpan di sessionStorage, bukan di URL.
 */
export function setLocale(next: Locale): void {
  try {
    localStorage.setItem(STORAGE_KEY, next);
  } catch {
    // Tanpa penyimpanan, pilihan hanya berlaku sampai halaman ditutup.
  }
  location.reload();
}

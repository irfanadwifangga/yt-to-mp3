export type Theme = "system" | "light" | "dark";

const STORAGE_KEY = "yt2mp3.theme";

/** Tema tersimpan; tanpa pilihan, tampilan mengikuti sistem operasi. */
export function loadTheme(): Theme {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "light" || stored === "dark") return stored;
  } catch {
    // Penyimpanan bisa diblokir browser; jatuh ke tema sistem.
  }
  return "system";
}

/**
 * Tema dipasang lewat atribut data-theme pada <html>. Tanpa atribut, CSS
 * memakai prefers-color-scheme, sehingga "ikuti sistem" tetap bereaksi
 * ketika pengguna mengganti mode gelap di sistem operasinya.
 */
export function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  if (theme === "system") delete root.dataset.theme;
  else root.dataset.theme = theme;
}

export function saveTheme(theme: Theme): void {
  try {
    if (theme === "system") localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    // Tanpa penyimpanan, pilihan hanya berlaku sampai halaman ditutup.
  }
  applyTheme(theme);
}

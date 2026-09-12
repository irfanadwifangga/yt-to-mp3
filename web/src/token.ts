const STORAGE_KEY = "yt2mp3.token";

/**
 * Mengambil session token dari query string saat pertama kali dibuka, lalu
 * menyimpannya dan membersihkan URL supaya token tidak tertinggal di address
 * bar maupun di history browser.
 */
export function bootstrapToken(): string | null {
  const url = new URL(window.location.href);
  const fromQuery = url.searchParams.get("token");

  if (fromQuery) {
    sessionStorage.setItem(STORAGE_KEY, fromQuery);
    url.searchParams.delete("token");
    window.history.replaceState({}, "", url.toString());
    return fromQuery;
  }

  return sessionStorage.getItem(STORAGE_KEY);
}

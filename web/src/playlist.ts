export function playlistURL(raw: string): string | null {
  try {
    const url = new URL(raw.trim());
    if (
      !["http:", "https:"].includes(url.protocol) ||
      ![
        "youtube.com",
        "m.youtube.com",
        "music.youtube.com",
        "youtu.be",
        "youtube-nocookie.com",
      ].includes(url.hostname.toLowerCase().replace(/^www\./, ""))
    )
      return null;
    const id = url.searchParams.get("list") ?? "";
    return /^[\w-]{2,128}$/.test(id) ? `https://www.youtube.com/playlist?list=${id}` : null;
  } catch {
    return null;
  }
}

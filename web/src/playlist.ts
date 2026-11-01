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
    if (!/^[\w-]{2,128}$/.test(id)) return null;
    if (id.startsWith("RD")) {
      const candidate =
        url.searchParams.get("v") ??
        (url.hostname === "youtu.be" ? url.pathname.slice(1).split("/")[0] : "");
      const seed = /^[\w-]{11}$/.test(candidate ?? "")
        ? candidate
        : /^RD[\w-]{11}$/.test(id)
          ? id.slice(2)
          : "";
      if (seed) return `https://www.youtube.com/watch?v=${seed}&list=${id}`;
    }
    return `https://www.youtube.com/playlist?list=${id}`;
  } catch {
    return null;
  }
}

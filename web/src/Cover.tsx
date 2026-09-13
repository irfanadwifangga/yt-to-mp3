import { useState, type CSSProperties } from "react";

/** Id video YouTube selalu 11 karakter dari alfabet base64url. */
const VIDEO_ID = /^[A-Za-z0-9_-]{11}$/;

/** Mengambil id video dari source key kanonik "youtube:<id>". */
export function videoId(sourceKey: string): string | null {
  const id = sourceKey.startsWith("youtube:") ? sourceKey.slice("youtube:".length) : "";
  return VIDEO_ID.test(id) ? id : null;
}

/**
 * Sampul diambil langsung dari CDN gambar YouTube, host yang sama yang
 * diizinkan CSP. URL dibentuk dari id alih-alih memakai thumbnail_url dari
 * metadata, karena yang terakhir bisa menunjuk host lain yang diblokir.
 */
function thumbnailUrl(id: string): string {
  return `https://i.ytimg.com/vi/${id}/mqdefault.jpg`;
}

export type CoverState = "active" | "waiting" | "done" | "dim";

interface Props {
  sourceKey: string;
  state: CoverState;
  /** Persen 0–100; null berarti total belum diketahui. */
  percent?: number | null;
  size?: "md" | "lg";
  /** Label aksesibel; hanya dipakai ketika sampul berperan sebagai progress. */
  label?: string;
}

/**
 * Sampul yang sekaligus menjadi indikator progress.
 *
 * Versi abu-abu terisi warna dari bawah ke atas mengikuti kemajuan job,
 * seperti level meter pada perangkat audio. Hasilnya, antrean terbaca dari
 * sampul-sampulnya saja tanpa perlu membaca angka.
 */
export function Cover({ sourceKey, state, percent, size = "md", label }: Props) {
  const id = videoId(sourceKey);
  const [broken, setBroken] = useState(false);
  const src = id && !broken ? thumbnailUrl(id) : null;

  const indeterminate = state === "active" && percent == null;
  const fill = state === "done" ? 100 : state === "active" ? Math.min(100, Math.max(0, percent ?? 0)) : 0;

  const className = [
    "cover",
    `cover-${state}`,
    size === "lg" ? "cover-lg" : "",
    indeterminate ? "is-indeterminate" : "",
  ]
    .filter(Boolean)
    .join(" ");

  const a11y =
    state === "active"
      ? {
          role: "progressbar",
          "aria-valuemin": 0,
          "aria-valuemax": 100,
          "aria-valuenow": percent == null ? undefined : Math.round(percent),
          "aria-label": label,
        }
      : { "aria-hidden": true };

  return (
    <div className={className} style={{ "--fill": `${fill}%` } as CSSProperties} {...a11y}>
      {src ? (
        <>
          <img
            className="cover-base"
            src={src}
            alt=""
            loading="lazy"
            referrerPolicy="no-referrer"
            onError={() => setBroken(true)}
          />
          <img className="cover-fill" src={src} alt="" loading="lazy" referrerPolicy="no-referrer" />
        </>
      ) : (
        // Tanpa sampul (offline, video dihapus), alur piringan hitam
        // menggantikannya supaya progress tetap terlihat.
        <>
          <span className="cover-base groove" />
          <span className="cover-fill groove" />
        </>
      )}
    </div>
  );
}

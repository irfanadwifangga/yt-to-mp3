import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type Health, type Metadata } from "./api";

/** Pesan dirakit di klien dari kode, bukan dari field message. ADR-027. */
const MESSAGES: Record<string, string> = {
  UNAUTHORIZED: "Session token tidak valid. Buka ulang aplikasi dari shortcut.",
  FORBIDDEN_HOST: "Host tidak diizinkan.",
  FORBIDDEN_ORIGIN: "Origin tidak diizinkan.",
  BAD_REQUEST: "Permintaan tidak dapat dibaca.",
  INVALID_URL: "URL tidak valid.",
  UNSUPPORTED_URL: "URL ini bukan tautan video YouTube.",
  LIVE_NOT_SUPPORTED: "Siaran langsung tidak didukung.",
  VIDEO_PRIVATE: "Video bersifat privat.",
  VIDEO_UNAVAILABLE: "Video tidak tersedia.",
  GEO_BLOCKED: "Video diblokir di wilayah ini.",
  AGE_RESTRICTED: "Video dibatasi usia dan tidak dapat diproses.",
  RATE_LIMITED: "Terlalu banyak permintaan. Coba lagi beberapa saat lagi.",
  TOOL_MISSING: "yt-dlp belum terpasang. Pasang dulu di bawah.",
  TOOL_OUTDATED: "yt-dlp perlu diperbarui.",
  TOOL_MANIFEST_INCOMPLETE: "Versi tool belum di-pin di manifest, jadi instalasi otomatis ditolak.",
  TOOL_CHECKSUM_MISMATCH: "Checksum unduhan tidak cocok. Instalasi dibatalkan.",
  TOOL_INSTALL_FAILED: "Instalasi tool gagal.",
  TIMEOUT: "Permintaan melewati batas waktu.",
  INTERNAL: "Terjadi kesalahan internal."
};

function messageFor(err: unknown, fallback: string): string {
  if (err instanceof ApiError) return MESSAGES[err.code] ?? `${fallback} (${err.code}).`;
  return "Tidak dapat menghubungi server.";
}

function formatDuration(ms: number): string {
  if (ms <= 0) return "tidak diketahui";
  const total = Math.round(ms / 1000);
  const m = Math.floor(total / 60);
  const s = total % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}

export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [quitting, setQuitting] = useState(false);

  const load = useCallback(async () => {
    try {
      setHealth(await api.health());
      setError(null);
    } catch (err) {
      setError(messageFor(err, "Gagal memuat status"));
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 5000);
    return () => clearInterval(timer);
  }, [load]);

  async function handleQuit() {
    setQuitting(true);
    try {
      await api.shutdown();
    } catch {
      // Server boleh mati sebelum respons sampai; itu bukan kegagalan.
    }
  }

  if (quitting) {
    return (
      <main className="shell">
        <h1>Aplikasi dihentikan</h1>
        <p className="muted">Tab ini sudah boleh ditutup.</p>
      </main>
    );
  }

  const ytdlpReady = health?.tools["yt-dlp"]?.available ?? false;

  return (
    <main className="shell">
      <header>
        <h1>yt-to-mp3</h1>
        {health && <span className="badge">v{health.version}</span>}
      </header>

      {error && <p className="error">{error}</p>}

      <Analyze ready={ytdlpReady} />
      <Tools health={health} onChanged={load} />

      {health && (
        <dl className="grid">
          <dt>Output</dt>
          <dd className="path">{health.output_dir}</dd>
          <dt>Antrean</dt>
          <dd>
            {health.queue.active} aktif / {health.queue.queued} menunggu (kapasitas{" "}
            {health.queue.capacity})
          </dd>
        </dl>
      )}

      <footer>
        <button type="button" onClick={handleQuit}>
          Keluar
        </button>
      </footer>
    </main>
  );
}

function Analyze({ ready }: { ready: boolean }) {
  const [url, setUrl] = useState("");
  const [result, setResult] = useState<Metadata | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      setResult(await api.metadata(url));
    } catch (err) {
      setError(messageFor(err, "Analisis gagal"));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section>
      <h2>Analisis</h2>
      <form onSubmit={handleSubmit} className="row">
        <input
          type="url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://www.youtube.com/watch?v=..."
          required
        />
        <button type="submit" disabled={busy || !ready}>
          {busy ? "Menganalisis..." : "Analisis"}
        </button>
      </form>

      {!ready && <p className="muted">Pasang yt-dlp dulu untuk mengaktifkan analisis.</p>}
      {error && <p className="error">{error}</p>}

      {result && (
        <dl className="grid">
          <dt>Judul</dt>
          <dd>{result.title}</dd>
          <dt>Channel</dt>
          <dd>{result.uploader || "tidak diketahui"}</dd>
          <dt>Durasi</dt>
          <dd>{formatDuration(result.duration_ms)}</dd>
          <dt>Codec sumber</dt>
          <dd>
            {result.source_codec || "tidak diketahui"}
            {result.sample_rate > 0 && ` @ ${result.sample_rate} Hz`}
          </dd>
        </dl>
      )}
    </section>
  );
}

function Tools({ health, onChanged }: { health: Health | null; onChanged: () => void }) {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function install(name: string) {
    setBusy(name);
    setError(null);
    try {
      await api.installTool(name);
      onChanged();
    } catch (err) {
      setError(messageFor(err, "Instalasi gagal"));
    } finally {
      setBusy(null);
    }
  }

  if (!health) return null;

  return (
    <section>
      <h2>Tool</h2>
      {error && <p className="error">{error}</p>}
      <ul className="tools">
        {Object.entries(health.tools).map(([name, tool]) => (
          <li key={name}>
            <span className={tool.available ? "dot ok" : "dot off"} />
            <span>{name}</span>
            <span className="muted">
              {tool.available ? tool.version || "terpasang" : "belum tersedia"}
            </span>
            {/* ffprobe ikut terpasang bersama ffmpeg dari arsip yang sama. */}
            {!tool.available && name !== "ffprobe" && (
              <button type="button" onClick={() => void install(name)} disabled={busy !== null}>
                {busy === name ? "Memasang..." : "Pasang"}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

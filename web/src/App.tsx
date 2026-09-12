import { useCallback, useEffect, useState } from "react";
import { api, ApiError, type Health } from "./api";

/** Pesan error dirakit di klien dari kode, bukan dari field message. ADR-027. */
const MESSAGES: Record<string, string> = {
  UNAUTHORIZED: "Session token tidak valid. Buka ulang aplikasi dari shortcut.",
  FORBIDDEN_HOST: "Host tidak diizinkan.",
  FORBIDDEN_ORIGIN: "Origin tidak diizinkan.",
  INTERNAL: "Terjadi kesalahan internal.",
};

function messageFor(err: unknown): string {
  if (err instanceof ApiError) {
    return MESSAGES[err.code] ?? `Gagal memuat status (${err.code}).`;
  }
  return "Tidak dapat menghubungi server.";
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
      setError(messageFor(err));
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

  return (
    <main className="shell">
      <header>
        <h1>yt-to-mp3</h1>
        {health && <span className="badge">v{health.version}</span>}
      </header>

      {error && <p className="error">{error}</p>}

      {health && (
        <>
          <dl className="grid">
            <dt>Status</dt>
            <dd>{health.status}</dd>
            <dt>Uptime</dt>
            <dd>{health.uptime_seconds} detik</dd>
            <dt>Output</dt>
            <dd className="path">{health.output_dir}</dd>
            <dt>Antrean</dt>
            <dd>
              {health.queue.active} aktif / {health.queue.queued} menunggu (kapasitas{" "}
              {health.queue.capacity})
            </dd>
          </dl>

          <h2>Tool</h2>
          <ul className="tools">
            {Object.entries(health.tools).map(([name, tool]) => (
              <li key={name}>
                <span className={tool.available ? "dot ok" : "dot off"} />
                {name}
                <span className="muted">
                  {tool.available ? tool.version : "belum tersedia"}
                </span>
              </li>
            ))}
          </ul>
        </>
      )}

      <footer>
        <button type="button" onClick={handleQuit}>
          Keluar
        </button>
      </footer>
    </main>
  );
}

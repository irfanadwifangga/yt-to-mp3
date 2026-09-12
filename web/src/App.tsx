import { useCallback, useEffect, useState } from "react";
import { api, isTerminal, type Health, type Job, type Metadata, type Preset } from "./api";
import { formatDuration, messageFor } from "./messages";
import { History, Queue } from "./Jobs";
import { Settings } from "./Settings";

/** Antrean disegarkan cukup sering untuk terasa hidup, tetapi progress
 *  halus datang lewat SSE sehingga polling tidak perlu rapat. */
const POLL_MS = 2000;

export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [presets, setPresets] = useState<Preset[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [quitting, setQuitting] = useState(false);

  const refresh = useCallback(async () => {
    try {
      const [h, j] = await Promise.all([api.health(), api.jobs()]);
      setHealth(h);
      setJobs(j.jobs);
      setError(null);
    } catch (err) {
      setError(messageFor(err, "Gagal memuat status"));
    }
  }, []);

  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), POLL_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  useEffect(() => {
    // Preset adalah product contract yang hidup di database; SPA tidak
    // boleh meng-hardcode-nya.
    api
      .presets()
      .then((res) => setPresets(res.presets))
      .catch(() => setPresets([]));
  }, []);

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

  const ready = health?.tools["yt-dlp"]?.available ?? false;
  const active = jobs.filter((j) => !isTerminal(j.status));
  const finished = jobs.filter((j) => isTerminal(j.status));

  return (
    <main className="shell">
      <header>
        <h1>yt-to-mp3</h1>
        {health && <span className="badge">v{health.version}</span>}
      </header>

      {error && <p className="error">{error}</p>}

      <Analyze ready={ready} presets={presets} onQueued={refresh} />
      <Queue jobs={active} onChanged={refresh} />
      <History jobs={finished} onChanged={refresh} />
      <Tools health={health} onChanged={refresh} />
      <Settings />

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
        <button type="button" onClick={() => void handleQuit()}>
          Keluar
        </button>
      </footer>
    </main>
  );
}

interface AnalyzeProps {
  ready: boolean;
  presets: Preset[];
  onQueued: () => void;
}

function Analyze({ ready, presets, onQueued }: AnalyzeProps) {
  const [url, setUrl] = useState("");
  const [presetId, setPresetId] = useState("");
  const [result, setResult] = useState<Metadata | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"analisis" | "antre" | null>(null);

  const selected = presetId || presets.find((p) => p.id === "mp3_standard")?.id || presets[0]?.id;

  async function handleAnalyze(e: React.FormEvent) {
    e.preventDefault();
    setBusy("analisis");
    setError(null);
    setResult(null);
    try {
      setResult(await api.metadata(url));
    } catch (err) {
      setError(messageFor(err, "Analisis gagal"));
    } finally {
      setBusy(null);
    }
  }

  async function handleConvert() {
    if (!selected) return;
    setBusy("antre");
    setError(null);
    try {
      await api.createJob(url, selected);
      setResult(null);
      setUrl("");
      onQueued();
    } catch (err) {
      setError(messageFor(err, "Tidak dapat mengantre"));
    } finally {
      setBusy(null);
    }
  }

  return (
    <section>
      <h2>Analisis</h2>
      <form onSubmit={(e) => void handleAnalyze(e)} className="row">
        <input
          type="url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://www.youtube.com/watch?v=..."
          required
        />
        <button type="submit" disabled={busy !== null || !ready}>
          {busy === "analisis" ? "Menganalisis..." : "Analisis"}
        </button>
      </form>

      {!ready && <p className="muted">Pasang yt-dlp dulu untuk mengaktifkan analisis.</p>}
      {error && <p className="error">{error}</p>}

      {result && (
        <>
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

          <div className="row">
            <select
              value={selected ?? ""}
              onChange={(e) => setPresetId(e.target.value)}
              aria-label="Preset"
            >
              {presets.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.label}
                  {p.bitrate_kbps ? ` — ${p.bitrate_kbps} kbps` : " — VBR"}
                </option>
              ))}
            </select>
            <button type="button" onClick={() => void handleConvert()} disabled={busy !== null}>
              {busy === "antre" ? "Mengantre..." : "Konversi"}
            </button>
          </div>
        </>
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

import { useCallback, useEffect, useState } from "react";
import { api, isTerminal, type Health, type Job, type Metadata, type Preset } from "./api";
import { locale, setLocale, t, type Locale } from "./i18n";
import { History, Queue } from "./Jobs";
import { formatDuration, messageFor } from "./messages";
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
      setError(messageFor(err, t("app.loadFailed")));
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
        <h1>{t("app.stopped.title")}</h1>
        <p className="muted">{t("app.stopped.hint")}</p>
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
          <dt>{t("app.output")}</dt>
          <dd className="path">{health.output_dir}</dd>
          <dt>{t("app.queue")}</dt>
          <dd>
            {t("app.queueSummary", {
              active: health.queue.active,
              queued: health.queue.queued,
              capacity: health.queue.capacity,
            })}
          </dd>
        </dl>
      )}

      <footer className="footer">
        <button type="button" onClick={() => void handleQuit()}>
          {t("app.quit")}
        </button>
        <label className="lang">
          <span className="muted small">{t("app.language")}</span>
          {/* Nama bahasa sengaja ditulis dalam bahasanya sendiri, supaya tetap
              terbaca oleh orang yang tidak memahami bahasa yang sedang aktif. */}
          <select
            value={locale}
            onChange={(e) => setLocale(e.target.value as Locale)}
            aria-label={t("app.language")}
          >
            <option value="id">Bahasa Indonesia</option>
            <option value="en">English</option>
          </select>
        </label>
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
  const [busy, setBusy] = useState<"analyze" | "queue" | null>(null);

  const selected = presetId || presets.find((p) => p.id === "mp3_standard")?.id || presets[0]?.id;

  async function handleAnalyze(e: React.FormEvent) {
    e.preventDefault();
    setBusy("analyze");
    setError(null);
    setResult(null);
    try {
      setResult(await api.metadata(url));
    } catch (err) {
      setError(messageFor(err, t("analyze.failed")));
    } finally {
      setBusy(null);
    }
  }

  async function handleConvert() {
    if (!selected) return;
    setBusy("queue");
    setError(null);
    try {
      await api.createJob(url, selected);
      setResult(null);
      setUrl("");
      onQueued();
    } catch (err) {
      setError(messageFor(err, t("analyze.queueFailed")));
    } finally {
      setBusy(null);
    }
  }

  return (
    <section>
      <h2>{t("analyze.title")}</h2>
      <form onSubmit={(e) => void handleAnalyze(e)} className="row">
        <input
          type="url"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://www.youtube.com/watch?v=..."
          required
        />
        <button type="submit" disabled={busy !== null || !ready}>
          {busy === "analyze" ? t("analyze.busy") : t("analyze.submit")}
        </button>
      </form>

      {!ready && <p className="muted">{t("analyze.needTool")}</p>}
      {error && <p className="error">{error}</p>}

      {result && (
        <>
          <dl className="grid">
            <dt>{t("analyze.field.title")}</dt>
            <dd>{result.title}</dd>
            <dt>{t("analyze.field.channel")}</dt>
            <dd>{result.uploader || t("analyze.unknown")}</dd>
            <dt>{t("analyze.field.duration")}</dt>
            <dd>{formatDuration(result.duration_ms)}</dd>
            <dt>{t("analyze.field.codec")}</dt>
            <dd>
              {result.source_codec || t("analyze.unknown")}
              {result.sample_rate > 0 && ` @ ${result.sample_rate} Hz`}
            </dd>
          </dl>

          <div className="row">
            <select
              value={selected ?? ""}
              onChange={(e) => setPresetId(e.target.value)}
              aria-label={t("analyze.preset")}
            >
              {presets.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.label}
                  {p.bitrate_kbps ? ` — ${p.bitrate_kbps} kbps` : " — VBR"}
                </option>
              ))}
            </select>
            <button type="button" onClick={() => void handleConvert()} disabled={busy !== null}>
              {busy === "queue" ? t("analyze.queueing") : t("analyze.convert")}
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
      setError(messageFor(err, t("tools.installFailed")));
    } finally {
      setBusy(null);
    }
  }

  if (!health) return null;

  return (
    <section>
      <h2>{t("tools.title")}</h2>
      {error && <p className="error">{error}</p>}
      <ul className="tools">
        {Object.entries(health.tools).map(([name, tool]) => (
          <li key={name}>
            <span className={tool.available ? "dot ok" : "dot off"} />
            <span>{name}</span>
            <span className="muted">
              {tool.available ? tool.version || t("tools.installed") : t("tools.unavailable")}
            </span>
            {/* ffprobe ikut terpasang bersama ffmpeg dari arsip yang sama. */}
            {!tool.available && name !== "ffprobe" && (
              <button type="button" onClick={() => void install(name)} disabled={busy !== null}>
                {busy === name ? t("tools.installing") : t("tools.install")}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}

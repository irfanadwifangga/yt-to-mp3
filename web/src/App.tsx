import { useCallback, useEffect, useState } from "react";
import { api, isTerminal, type Health, type Job, type Preset } from "./api";
import { Capture } from "./Capture";
import { GearIcon } from "./icons";
import { t } from "./i18n";
import { ActiveList, FinishedList } from "./Jobs";
import { messageFor } from "./messages";
import { SettingsDialog, type SettingsSection } from "./SettingsDialog";

/** Antrean disegarkan cukup sering untuk terasa hidup, tetapi progress
 *  halus datang lewat SSE sehingga polling tidak perlu rapat. */
const POLL_MS = 2000;

export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [presets, setPresets] = useState<Preset[]>([]);
  const [defaultPreset, setDefaultPreset] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [quitting, setQuitting] = useState(false);
  const [settingsSection, setSettingsSection] = useState<SettingsSection | null>(null);

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

  // Preset adalah product contract yang hidup di database, dan preset bawaan
  // bisa diganti pengguna; keduanya dibaca dari server, bukan di-hardcode.
  const loadPresets = useCallback(async () => {
    try {
      const [p, s] = await Promise.all([api.presets(), api.settings()]);
      setPresets(p.presets);
      setDefaultPreset(s.settings.find((x) => x.key === "default_preset_id")?.value ?? "");
    } catch {
      // Kartu tetap bisa dipakai dengan preset pertama.
    }
  }, []);

  useEffect(() => {
    void refresh();
    const timer = setInterval(() => void refresh(), POLL_MS);
    return () => clearInterval(timer);
  }, [refresh]);

  useEffect(() => {
    void loadPresets();
  }, [loadPresets]);

  // Ctrl+, membuka setelan, mengikuti kebiasaan aplikasi desktop.
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.ctrlKey || e.metaKey) && e.key === ",") {
        e.preventDefault();
        setSettingsSection("storage");
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const active = jobs.filter((j) => !isTerminal(j.status));
  const finished = jobs.filter((j) => isTerminal(j.status));

  // Jumlah job berjalan tampil di judul tab, terlihat walau tab di belakang.
  useEffect(() => {
    document.title = active.length > 0 ? `(${active.length}) yt-to-mp3` : "yt-to-mp3";
  }, [active.length]);

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
      <main className="stopped">
        <span className="brand-mark" aria-hidden="true" />
        <h1>{t("app.stopped.title")}</h1>
        <p>{t("app.stopped.hint")}</p>
      </main>
    );
  }

  const tools = health ? Object.values(health.tools) : [];
  const toolsReady = tools.length > 0 && tools.every((tool) => tool.available);
  const canAnalyze = health?.tools["yt-dlp"]?.available ?? false;

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true" />
          yt-to-mp3
        </div>

        <div className="topbar-actions">
          {health && (
            <button
              type="button"
              className={`chip ${toolsReady ? "ok" : "warn"}`}
              onClick={() => setSettingsSection("tools")}>
              <span className={toolsReady ? "dot ok" : "dot warn"} />
              {toolsReady ? t("app.toolsReady") : t("app.toolsMissing")}
            </button>
          )}
          <button
            type="button"
            className="btn"
            onClick={() => setSettingsSection("storage")}
            aria-keyshortcuts="Control+Comma">
            <GearIcon />
            {t("app.settings")}
          </button>
          <button type="button" className="btn ghost" onClick={() => void handleQuit()}>
            {t("app.quit")}
          </button>
        </div>
      </header>

      <main className="content">
        {error && (
          <p className="alert" role="alert">
            {error}
          </p>
        )}

        {health && !toolsReady && (
          <div className="setup">
            <div>
              <p className="setup-title">{t("app.setupTitle")}</p>
              <p className="hint">{t("app.setupHint")}</p>
            </div>
            <button
              type="button"
              className="btn primary"
              onClick={() => setSettingsSection("tools")}>
              {t("app.setupAction")}
            </button>
          </div>
        )}

        <Capture
          ready={canAnalyze}
          presets={presets}
          defaultPreset={defaultPreset}
          onQueued={refresh}
        />
        <ActiveList jobs={active} onChanged={refresh} />
        <FinishedList jobs={finished} onChanged={refresh} />
      </main>

      {health && (
        <footer className="statusbar">
          <span className="statusbar-label">{t("app.savedTo")}</span>
          <code className="path" title={health.output_dir}>
            {health.output_dir}
          </code>
          <button type="button" className="link" onClick={() => setSettingsSection("storage")}>
            {t("app.change")}
          </button>
          <span className="statusbar-queue mono">
            {t("app.queueSummary", {
              active: health.queue.active,
              queued: health.queue.queued,
              capacity: health.queue.capacity
            })}
          </span>
        </footer>
      )}

      <SettingsDialog
        section={settingsSection}
        health={health}
        presets={presets}
        onClose={() => setSettingsSection(null)}
        onChanged={() => {
          void refresh();
          void loadPresets();
        }}
      />
    </div>
  );
}

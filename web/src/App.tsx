import { useCallback, useEffect, useRef, useState } from "react";
import { api, type Health, type Job, type Preset } from "./api";
import { Capture } from "./Capture";
import { GearIcon } from "./icons";
import { t } from "./i18n";
import { ActiveList, FinishedList } from "./Jobs";
import { messageFor } from "./messages";
import { SettingsDialog, type SettingsSection } from "./SettingsDialog";

/** Antrean disegarkan cukup sering untuk terasa hidup, tetapi progress
 *  halus datang lewat SSE sehingga polling tidak perlu rapat. */
const POLL_MS = 2000;

/** Batas antrean di setelan maksimal 500, tetapi job aktif sekaligus
 *  jarang lebih dari puluhan. */
const ACTIVE_LIMIT = 100;
const PAGE_SIZE = 25;

/**
 * Menggabungkan dua daftar job tanpa duplikat, terbaru di atas.
 *
 * Baris dari `fresh` menang karena statusnya lebih baru. Tanggal dibanding
 * sebagai waktu, bukan string: pecahan detik RFC 3339 dari server panjangnya
 * tidak tetap.
 */
function mergeJobs(fresh: Job[], previous: Job[]): Job[] {
  const byId = new Map<string, Job>();
  for (const job of previous) byId.set(job.id, job);
  for (const job of fresh) byId.set(job.id, job);
  return [...byId.values()].sort(
    (a, b) => Date.parse(b.created_at) - Date.parse(a.created_at) || b.id.localeCompare(a.id)
  );
}

export function App() {
  const [health, setHealth] = useState<Health | null>(null);
  const [active, setActive] = useState<Job[]>([]);
  const [finished, setFinished] = useState<Job[]>([]);
  const [cursor, setCursor] = useState<string | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  const [presets, setPresets] = useState<Preset[]>([]);
  const [defaultPreset, setDefaultPreset] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [quitting, setQuitting] = useState(false);
  const [settingsSection, setSettingsSection] = useState<SettingsSection | null>(null);

  // Setelah pengguna memuat halaman lama, polling tidak lagi mengganti
  // riwayat dengan halaman pertama saja. Baris yang tergeser keluar dari
  // halaman pertama karena job baru selesai tetap disimpan, sehingga tidak
  // ada celah di antara halaman pertama dan halaman lama yang sudah dimuat.
  const extended = useRef(false);

  const refresh = useCallback(async () => {
    try {
      const [h, a, f] = await Promise.all([
        api.health(),
        api.jobs({ scope: "active", limit: ACTIVE_LIMIT }),
        api.jobs({ scope: "finished", limit: PAGE_SIZE })
      ]);
      setHealth(h);
      setActive(a.jobs);
      if (extended.current) {
        setFinished((prev) => mergeJobs(f.jobs, prev));
      } else {
        setFinished(f.jobs);
        setCursor(f.next_cursor);
      }
      setError(null);
    } catch (err) {
      setError(messageFor(err, t("app.loadFailed")));
    }
  }, []);

  async function loadMore() {
    if (!cursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const page = await api.jobs({ scope: "finished", limit: PAGE_SIZE, cursor });
      extended.current = true;
      setFinished((prev) => mergeJobs(prev, page.jobs));
      setCursor(page.next_cursor);
    } catch (err) {
      setError(messageFor(err, t("history.loadMoreFailed")));
    } finally {
      setLoadingMore(false);
    }
  }

  const removeFinished = useCallback((id: string) => {
    setFinished((prev) => prev.filter((job) => job.id !== id));
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
  const toolsUpdate = tools.some((tool) => tool.update_available);
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
              className={`chip ${toolsReady && !toolsUpdate ? "ok" : "warn"}`}
              onClick={() => setSettingsSection("tools")}>
              <span className={toolsReady && !toolsUpdate ? "dot ok" : "dot warn"} />
              {!toolsReady
                ? t("app.toolsMissing")
                : toolsUpdate
                  ? t("app.toolsUpdate")
                  : t("app.toolsReady")}
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
        <FinishedList
          jobs={finished}
          onChanged={refresh}
          onRemoved={removeFinished}
          hasMore={Boolean(cursor)}
          loadingMore={loadingMore}
          onLoadMore={() => void loadMore()}
        />
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

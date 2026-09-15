import { useCallback, useEffect, useRef, useState } from "react";
import { api, updateYtdlpAndRetry, type Health, type Job, type Preset } from "./api";
import { Capture } from "./Capture";
import { GearIcon } from "./icons";
import { t } from "./i18n";
import { ActiveList, FinishedList } from "./Jobs";
import { mergeJobs, newlyFinished, pendingIds } from "./jobList";
import { messageFor, messageForCode } from "./messages";
import { SettingsDialog, type SettingsSection } from "./SettingsDialog";
import { Toasts, type Toast } from "./Toasts";

/**
 * Nama aplikasi yang dilihat pengguna, sama dengan version.DisplayName di
 * backend. Nama teknis yt-to-mp3 (folder data, exe) sengaja tidak dipakai di UI.
 */
const APP_NAME = "Youtube To MP3 Converter";

/** Notifikasi yang terlihat sekaligus; yang lebih lama digeser keluar. */
const MAX_TOASTS = 4;

/** Antrean disegarkan cukup sering untuk terasa hidup, tetapi progress
 *  halus datang lewat SSE sehingga polling tidak perlu rapat. */
const POLL_MS = 2000;

/** Batas antrean di setelan maksimal 500, tetapi job aktif sekaligus
 *  jarang lebih dari puluhan. */
const ACTIVE_LIMIT = 100;
const PAGE_SIZE = 25;

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
  const [toasts, setToasts] = useState<Toast[]>([]);

  // Id job yang terlihat aktif pada polling sebelumnya. Job yang keluar dari
  // daftar ini lalu muncul di riwayat berarti baru saja selesai atau gagal.
  // null sampai polling pertama, supaya riwayat lama tidak ikut diumumkan
  // saat halaman dibuka.
  const knownActive = useRef<Set<string> | null>(null);

  const pushToast = useCallback((toast: Toast) => {
    setToasts((prev) => [...prev.filter((x) => x.id !== toast.id), toast].slice(-MAX_TOASTS));
  }, []);

  const dismissToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((x) => x.id !== id));
  }, []);

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

      const nowActive = new Set(a.jobs.map((job) => job.id));
      const previous = knownActive.current;
      if (previous) {
        for (const job of newlyFinished(previous, f.jobs, nowActive)) {
          const title = job.title || job.source_key;
          // Berkas sudah tersimpan di folder hasil; notifikasi ini yang
          // memberi tahu pengguna, bukan tombol unduh di riwayat.
          if (job.status === "completed") {
            pushToast({
              id: `done-${job.id}`,
              tone: "ok",
              title: t("toast.completed", { title }),
              body: job.file_name
                ? t("toast.savedAs", { file: job.file_name })
                : t("toast.savedGeneric"),
              action: job.file_id
                ? {
                    label: t("history.reveal"),
                    icon: "folder",
                    run: () => api.revealFile(job.file_id!)
                  }
                : undefined
            });
          } else if (job.status === "failed") {
            pushToast({
              id: `fail-${job.id}`,
              tone: "bad",
              title: t("toast.failed", { title }),
              body: messageForCode(job.error_code),
              // yt-dlp usang adalah penyebab kegagalan paling umum, dan
              // solusinya ada di aplikasi ini sendiri.
              action:
                job.error_code === "TOOL_OUTDATED"
                  ? { label: t("history.updateAndRetry"), run: () => updateYtdlpAndRetry(job.id) }
                  : undefined
            });
          }
        }
      }
      // Job yang sudah diumumkan keluar dari himpunan dengan sendirinya,
      // karena himpunan baru hanya berisi job yang masih aktif.
      knownActive.current = previous ? new Set([...nowActive, ...pendingIds(previous, f.jobs, nowActive)]) : nowActive;
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
  }, [pushToast]);

  // Job baru dicatat sebagai aktif saat itu juga. Konversi yang selesai
  // sebelum polling berikutnya tetap diumumkan walau tidak pernah terlihat
  // di daftar aktif.
  const handleQueued = useCallback(
    (job: Job) => {
      knownActive.current?.add(job.id);
      pushToast({
        id: `queued-${job.id}`,
        tone: "info",
        title: t("toast.queued", { title: job.title || job.source_key })
      });
      void refresh();
    },
    [pushToast, refresh]
  );

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

  // Jumlah job berjalan tampil di judul jendela, terlihat walau jendela di belakang.
  useEffect(() => {
    document.title = active.length > 0 ? `(${active.length}) ${APP_NAME}` : APP_NAME;
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
  // null selama health pertama belum datang: belum diketahui, bukan tidak ada.
  const canAnalyze = health ? (health.tools["yt-dlp"]?.available ?? false) : null;

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true" />
          {APP_NAME}
        </div>

        <div className="topbar-actions">
          {/* Aplikasi tidak memperbarui dirinya sendiri; cukup memberi tahu
              dan menunjuk ke halaman rilis. */}
          {health?.app_update.update_available && health.app_update.release_url && (
            <a
              className="chip warn"
              href={health.app_update.release_url}
              target="_blank"
              rel="noopener noreferrer">
              <span className="dot warn" />
              {t("app.updateAvailable", { version: health.app_update.latest ?? "" })}
            </a>
          )}
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
          onQueued={handleQueued}
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

      <Toasts toasts={toasts} onDismiss={dismissToast} />

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

import { useCallback, useEffect, useRef, useState } from "react";
import { api, ApiError, updateYtdlpAndRetry, type Health, type Job, type Preset } from "./api";
import { Capture } from "./Capture";
import { GearIcon, TerminalIcon, FolderIcon, PowerIcon } from "./icons";
import { t, locale, setLocale, type Locale } from "./i18n";
import { JobsTable } from "./JobsTable";
import { Modal } from "./Modal";
import { LogsPanel } from "./LogsPanel";
import { newlyFinished, pendingIds } from "./jobList";
import { messageFor, messageForCode } from "./messages";
import { SettingsDialog, type SettingsSection } from "./SettingsDialog";
import { Toasts, type Toast } from "./Toasts";

/**
 * Nama aplikasi yang dilihat pengguna, sama dengan version.DisplayName di
 * backend. Nama teknis yt-to-mp3 (folder data, exe) sengaja tidak dipakai di UI.
 */
const APP_NAME = "YouTube to MP3 & MP4";

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
  const [captureHost, setCaptureHost] = useState<HTMLDivElement | null>(null);
  const [health, setHealth] = useState<Health | null>(null);
  const [active, setActive] = useState<Job[]>([]);
  const [revision, setRevision] = useState(0);
  const [logsOpen, setLogsOpen] = useState(false);
  const [manualFolder, setManualFolder] = useState(false);
  const [folderDraft, setFolderDraft] = useState("");
  const [folderError, setFolderError] = useState("");
  const [picking, setPicking] = useState(false);
  const pickingRef = useRef(false);
  const [language, setLanguage] = useState(locale);
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

  const refreshRequest = useRef<AbortController | null>(null);

  const refresh = useCallback(async () => {
    if (refreshRequest.current) return;
    const controller = new AbortController();
    refreshRequest.current = controller;
    try {
      const [h, a, f] = await Promise.all([
        api.health(controller.signal),
        api.jobs({ scope: "active", limit: ACTIVE_LIMIT }, controller.signal),
        api.jobs({ scope: "finished", limit: PAGE_SIZE }, controller.signal),
      ]);
      if (controller.signal.aborted) return;
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
                    run: () => api.revealFile(job.file_id!),
                  }
                : undefined,
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
                  : undefined,
            });
          }
        }
      }
      // Job yang sudah diumumkan keluar dari himpunan dengan sendirinya,
      // karena himpunan baru hanya berisi job yang masih aktif.
      knownActive.current = previous
        ? new Set([...nowActive, ...pendingIds(previous, f.jobs, nowActive)])
        : nowActive;
      setError(null);
    } catch (err) {
      if (!controller.signal.aborted)
        setError(
          err instanceof ApiError && err.status === 401
            ? t("app.sessionExpired")
            : t("app.connectionLost"),
        );
    } finally {
      if (refreshRequest.current === controller) refreshRequest.current = null;
    }
  }, [pushToast]);

  // Job baru dicatat sebagai aktif saat itu juga. Konversi yang selesai
  // sebelum polling berikutnya tetap diumumkan walau tidak pernah terlihat
  // di daftar aktif.
  const handleQueued = useCallback(
    (job: Job) => {
      knownActive.current?.add(job.id);
      setRevision((n) => n + 1);
      pushToast({
        id: `queued-${job.id}`,
        tone: "info",
        title: t("toast.queued", { title: job.title || job.source_key }),
      });
      void refresh();
    },
    [pushToast, refresh],
  );

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
    function resume() {
      if (document.visibilityState === "hidden") return;
      refreshRequest.current?.abort();
      refreshRequest.current = null;
      void refresh();
      void loadPresets();
    }
    window.addEventListener("focus", resume);
    window.addEventListener("online", resume);
    document.addEventListener("visibilitychange", resume);
    return () => {
      clearInterval(timer);
      refreshRequest.current?.abort();
      refreshRequest.current = null;
      window.removeEventListener("focus", resume);
      window.removeEventListener("online", resume);
      document.removeEventListener("visibilitychange", resume);
    };
  }, [refresh, loadPresets]);

  useEffect(() => {
    void loadPresets();
    if (presets.length > 0) return;
    const timer = setInterval(() => void loadPresets(), 5000);
    return () => clearInterval(timer);
  }, [loadPresets, presets.length]);

  useEffect(() => {
    const change = () => setLanguage(locale);
    window.addEventListener("localechange", change);
    return () => window.removeEventListener("localechange", change);
  }, []);

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
    if (quitting) return;
    setQuitting(true);
    try {
      await api.shutdown();
      window.close();
    } catch (err) {
      setQuitting(false);
      setError(messageFor(err));
    }
  }

  async function saveFolder(path: string) {
    await api.updateSettings({ output_dir: path });
    setHealth((previous) => (previous ? { ...previous, output_dir: path } : previous));
    setManualFolder(false);
    void refresh();
  }
  async function chooseFolder() {
    if (pickingRef.current) return;
    pickingRef.current = true;
    setPicking(true);
    setFolderError("");
    try {
      const result = await api.pickFolder(t("settings.pickFolderTitle"), health?.output_dir ?? "");
      if (!result.cancelled && result.path) await saveFolder(result.path);
    } catch (err) {
      if (err instanceof ApiError && err.code === "DIALOG_UNAVAILABLE") {
        setFolderDraft(health?.output_dir ?? "");
        setManualFolder(true);
      } else setFolderError(messageFor(err));
    } finally {
      pickingRef.current = false;
      setPicking(false);
    }
  }
  async function submitFolder() {
    if (pickingRef.current || !folderDraft.trim()) return;
    pickingRef.current = true;
    setPicking(true);
    setFolderError("");
    try {
      await saveFolder(folderDraft.trim());
    } catch (err) {
      setFolderError(messageFor(err));
    } finally {
      pickingRef.current = false;
      setPicking(false);
    }
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
          <img className="brand-logo" src="/icon.svg" alt="" />
          {APP_NAME}
        </div>

        <div className="capture-host" ref={setCaptureHost} />
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
            className="toolbar-icon"
            onClick={() => setLogsOpen(true)}
            aria-label={t("app.logs")}
            data-tooltip={t("app.logsHint")}>
            <TerminalIcon />
          </button>
          <div className="language-toggle" role="group" aria-label={t("settings.label.language")}>
            {(["id", "en"] as Locale[]).map((code) => (
              <button
                key={code}
                type="button"
                aria-pressed={language === code}
                aria-label={code === "id" ? "Bahasa Indonesia" : "English"}
                onClick={() => setLocale(code)}>
                {code.toUpperCase()}
              </button>
            ))}
          </div>
          <span className="toolbar-divider" aria-hidden="true" />
          <button
            type="button"
            className="toolbar-icon"
            aria-label={t("app.settings")}
            data-tooltip={t("app.settings")}
            onClick={() => setSettingsSection("storage")}
            aria-keyshortcuts="Control+Comma">
            <GearIcon />
          </button>
          <span className="toolbar-divider" aria-hidden="true" />
          <button
            type="button"
            className="toolbar-icon toolbar-power"
            aria-label={t("app.quit")}
            data-tooltip={t("app.quit")}
            disabled={quitting}
            aria-busy={quitting}
            onClick={() => void handleQuit()}>
            <PowerIcon />
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
          toolbarHost={captureHost}
          ready={canAnalyze}
          presets={presets}
          defaultPreset={defaultPreset}
          onQueued={handleQueued}
        />
        <JobsTable presets={presets} refreshKey={revision} />
      </main>

      {health && (
        <footer className="statusbar">
          <span className="statusbar-label">{t("app.savedTo")}</span>
          <code className="path" title={health.output_dir}>
            {health.output_dir}
          </code>
          <button
            type="button"
            className="btn small"
            disabled={picking}
            onClick={() => void chooseFolder()}>
            <FolderIcon />
            {picking ? t("settings.picking") : t("app.change")}
          </button>
          <span className="connection-status" role="status">
            {error ? t("app.reconnecting") : t("app.connected")}
          </span>
          <span className="statusbar-queue mono">
            {t("app.queueSummary", {
              active: health.queue.active,
              queued: health.queue.queued,
              capacity: health.queue.capacity,
            })}
          </span>
        </footer>
      )}

      {folderError && !manualFolder && (
        <p className="alert" role="alert">
          {folderError}
        </p>
      )}
      {logsOpen && (
        <Modal title={t("app.logs")} onClose={() => setLogsOpen(false)}>
          <LogsPanel />
        </Modal>
      )}
      {manualFolder && (
        <Modal
          title={t("storage.title")}
          onClose={() => {
            if (!picking) setManualFolder(false);
          }}>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void submitFolder();
            }}
            className="folder-form">
            <label>
              {t("storage.manual")}
              <input
                value={folderDraft}
                onChange={(e) => setFolderDraft(e.target.value)}
                autoFocus
                disabled={picking}
              />
            </label>
            {folderError && (
              <p className="alert" role="alert">
                {folderError}
              </p>
            )}
            <button type="submit" className="btn primary" disabled={picking || !folderDraft.trim()}>
              {t("storage.save")}
            </button>
          </form>
        </Modal>
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

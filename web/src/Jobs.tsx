import { useEffect, useState } from "react";
import {
  api,
  downloadFile,
  isTerminal,
  MAX_AUTO_RETRIES,
  updateYtdlpAndRetry,
  type Job,
  type Preset
} from "./api";
import { Cover } from "./Cover";
import { DownloadIcon, FolderIcon } from "./icons";
import { t } from "./i18n";
import {
  formatTime,
  messageFor,
  messageForCode,
  phaseLabel,
  presetFullLabel,
  statusLabel
} from "./messages";
import { useJobStream } from "./useJobStream";

interface Props {
  jobs: Job[];
  /** Untuk menampilkan format dan kualitas tiap job, misalnya "MP4 · 720p". */
  presets: Preset[];
  onChanged: () => void;
}

/** Format dan kualitas job; kosong untuk preset yang sudah tidak ditawarkan. */
function formatLabel(presets: Preset[], job: Job): string {
  const preset = presets.find((p) => p.id === job.preset_id);
  return preset ? presetFullLabel(preset) : "";
}

/** Job yang masih berjalan atau menunggu giliran. */
export function ActiveList({ jobs, presets, onChanged }: Props) {
  return (
    <section className="block" aria-labelledby="active-title">
      <h2 id="active-title" className="block-title">
        {t("queue.title")}
        {jobs.length > 0 && <span className="count">{jobs.length}</span>}
      </h2>
      {jobs.length === 0 ? (
        <p className="empty">{t("queue.empty")}</p>
      ) : (
        <ul className="list">
          {jobs.map((job) => (
            <ActiveJob
              key={job.id}
              job={job}
              format={formatLabel(presets, job)}
              onChanged={onChanged}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

/** Detik tersisa sampai auto-retry, dibulatkan ke atas. */
function secondsUntil(iso: string): number {
  return Math.max(0, Math.ceil((Date.parse(iso) - Date.now()) / 1000));
}

function ActiveJob({
  job,
  format,
  onChanged
}: {
  job: Job;
  format: string;
  onChanged: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Progress live datang lewat SSE; nilai dari polling jadi cadangan bila
  // stream terputus.
  const stream = useJobStream(job.id, !isTerminal(job.status));
  const percent = stream.percent !== undefined ? stream.percent : job.progress;
  const phase = stream.phase ?? job.phase;
  const status = stream.status ?? job.status;
  const title = job.title || job.source_key;

  // Job yang selesai langsung dipindah ke daftar selesai, tanpa menunggu
  // polling berikutnya.
  useEffect(() => {
    if (stream.done) onChanged();
  }, [stream.done, onChanged]);

  async function handleCancel() {
    setBusy(true);
    setError(null);
    try {
      await api.cancelJob(job.id);
      onChanged();
    } catch (err) {
      setError(messageFor(err, t("queue.cancelFailed")));
    } finally {
      setBusy(false);
    }
  }

  const waiting = status === "queued";
  // Job yang menunggu jeda auto-retry membawa kode kegagalan terakhirnya,
  // supaya pengguna tahu kenapa job itu belum jalan.
  const retrying = waiting && Boolean(job.retry_at) && Boolean(job.error_code);

  return (
    <li className="item">
      <Cover
        sourceKey={job.source_key}
        state={waiting ? "waiting" : "active"}
        percent={percent}
        label={t("queue.progressLabel", { title })}
      />

      <div className="item-body">
        <p className="item-title" title={title}>
          {title}
        </p>
        <p className="item-meta">
          {retrying ? (
            <span className="mono">
              {t("queue.retryScheduled", {
                seconds: secondsUntil(job.retry_at!),
                attempt: job.attempt_count,
                max: MAX_AUTO_RETRIES
              })}
            </span>
          ) : (
            <span>{phaseLabel(phase) || statusLabel(status)}</span>
          )}
          {!waiting && (
            <span className="mono">
              {percent == null ? t("queue.calculating") : `${percent.toFixed(0)}%`}
            </span>
          )}
          {format && <span className="mono">{format}</span>}
        </p>
        {retrying && <p className="item-hint">{messageForCode(job.error_code)}</p>}
        {error && (
          <p className="alert small" role="alert">
            {error}
          </p>
        )}
      </div>

      <div className="item-actions">
        <button
          type="button"
          className="btn ghost small"
          onClick={() => void handleCancel()}
          disabled={busy || status === "cancelling"}>
          {busy || status === "cancelling" ? t("queue.cancelling") : t("queue.cancel")}
        </button>
      </div>
    </li>
  );
}

interface FinishedProps extends Props {
  onRemoved: (id: string) => void;
  hasMore: boolean;
  loadingMore: boolean;
  onLoadMore: () => void;
}

/** Job yang sudah selesai, gagal, atau dibatalkan. */
export function FinishedList({
  jobs,
  presets,
  onChanged,
  onRemoved,
  hasMore,
  loadingMore,
  onLoadMore
}: FinishedProps) {
  return (
    <section className="block" aria-labelledby="finished-title">
      <h2 id="finished-title" className="block-title">
        {t("history.title")}
        {jobs.length > 0 && <span className="count">{jobs.length}</span>}
      </h2>
      {jobs.length === 0 ? (
        <p className="empty">{t("history.empty")}</p>
      ) : (
        <ul className="list">
          {jobs.map((job) => (
            <FinishedJob
              key={job.id}
              job={job}
              preset={presets.find((p) => p.id === job.preset_id)}
              onChanged={onChanged}
              onRemoved={onRemoved}
            />
          ))}
        </ul>
      )}
      {hasMore && (
        <button
          type="button"
          className="btn ghost load-more"
          onClick={onLoadMore}
          disabled={loadingMore}>
          {loadingMore ? t("history.loadingMore") : t("history.loadMore")}
        </button>
      )}
    </section>
  );
}

/** Kegagalan permanen tidak akan berubah hasilnya bila diulang. */
const PERMANENT = new Set([
  "VIDEO_PRIVATE",
  "VIDEO_UNAVAILABLE",
  "GEO_BLOCKED",
  "AGE_RESTRICTED",
  "LIVE_NOT_SUPPORTED",
  "UNSUPPORTED_URL",
  "INVALID_URL"
]);

/** Konfirmasi hapus kembali ke keadaan semula bila diabaikan. */
const CONFIRM_TIMEOUT_MS = 5000;

type Action = "download" | "reveal" | "retry" | "update" | "delete";

interface FinishedJobProps {
  job: Job;
  preset?: Preset;
  onChanged: () => void;
  onRemoved: (id: string) => void;
}

function FinishedJob({ job, preset, onChanged, onRemoved }: FinishedJobProps) {
  const [busy, setBusy] = useState<Action | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    if (!confirming) return;
    const timer = setTimeout(() => setConfirming(false), CONFIRM_TIMEOUT_MS);
    return () => clearTimeout(timer);
  }, [confirming]);

  async function run(action: Action, fn: () => Promise<unknown>) {
    setBusy(action);
    setError(null);
    try {
      await fn();
      onChanged();
    } catch (err) {
      setError(messageFor(err, t("history.actionFailed")));
    } finally {
      setBusy(null);
      setConfirming(false);
    }
  }

  function remove(withFile: boolean) {
    void run("delete", async () => {
      await api.deleteJob(job.id, withFile);
      // Baris dari halaman riwayat lama tidak ikut tersegarkan polling,
      // jadi dibuang langsung dari daftar.
      onRemoved(job.id);
    });
  }

  const title = job.title || job.source_key;
  const done = job.status === "completed";
  const hasFile = done && Boolean(job.file_id);
  const retryable = !done && !PERMANENT.has(job.error_code ?? "");
  const tone = done ? "ok" : job.status === "failed" ? "bad" : "off";

  return (
    <li className="item">
      <Cover sourceKey={job.source_key} state={done ? "done" : "dim"} />

      <div className="item-body">
        <p className="item-title" title={title}>
          {title}
        </p>
        <p className="item-meta">
          <span className={`tag tag-${tone}`}>{statusLabel(job.status)}</span>
          <span className="mono">{formatTime(job.finished_at ?? job.created_at)}</span>
          {preset && <span className="mono">{presetFullLabel(preset)}</span>}
        </p>
        {job.error_code && job.status === "failed" && (
          <p className="item-error">{messageForCode(job.error_code)}</p>
        )}
        {hasFile && job.file_name && (
          <p className="item-hint mono item-file" title={job.file_name}>
            {job.file_name}
          </p>
        )}
        {done && !job.file_id && <p className="item-error">{t("history.missing")}</p>}
        {error && (
          <p className="alert small" role="alert">
            {error}
          </p>
        )}
      </div>

      <div className="item-actions">
        {confirming ? (
          hasFile ? (
            <>
              <span className="confirm-text">{t("history.confirmDeleteWithFile")}</span>
              <button
                type="button"
                className="btn small"
                disabled={busy !== null}
                onClick={() => remove(false)}>
                {t("history.removeOnly")}
              </button>
              <button
                type="button"
                className="btn danger small"
                disabled={busy !== null}
                onClick={() => remove(true)}>
                {t("history.removeWithFile")}
              </button>
              <button
                type="button"
                className="btn ghost small"
                onClick={() => setConfirming(false)}>
                {t("history.confirmNo")}
              </button>
            </>
          ) : (
            <>
              <span className="confirm-text">{t("history.confirmDelete")}</span>
              <button
                type="button"
                className="btn danger small"
                disabled={busy !== null}
                onClick={() => remove(false)}>
                {t("history.confirmYes")}
              </button>
              <button
                type="button"
                className="btn ghost small"
                onClick={() => setConfirming(false)}>
                {t("history.confirmNo")}
              </button>
            </>
          )
        ) : (
          <>
            {hasFile && (
              <>
                {/* Berkasnya sudah ada di folder hasil, jadi membuka folder
                    adalah aksi utama. Simpan salinan hanya mengunduh duplikat
                    lewat browser dan sengaja dibuat samar. */}
                <button
                  type="button"
                  className="btn small"
                  disabled={busy !== null}
                  onClick={() => void run("reveal", () => api.revealFile(job.file_id!))}>
                  <FolderIcon />
                  {t("history.reveal")}
                </button>
                <button
                  type="button"
                  className="btn ghost small"
                  disabled={busy !== null}
                  title={t("history.saveCopyHint")}
                  onClick={() =>
                    void run("download", () =>
                      downloadFile(job.file_id!, job.file_name || `${title}.${preset?.format ?? "mp3"}`)
                    )
                  }>
                  <DownloadIcon />
                  {busy === "download" ? t("history.preparing") : t("history.saveCopy")}
                </button>
              </>
            )}

            {/* Mengulang tanpa memperbarui yt-dlp pasti gagal lagi, jadi
                kegagalan karena yt-dlp usang menawarkan keduanya sekaligus. */}
            {retryable && job.error_code === "TOOL_OUTDATED" ? (
              <button
                type="button"
                className="btn small primary"
                disabled={busy !== null}
                onClick={() => void run("update", () => updateYtdlpAndRetry(job.id))}>
                {busy === "update" ? t("tools.updating") : t("history.updateAndRetry")}
              </button>
            ) : (
              retryable && (
                <button
                  type="button"
                  className="btn small"
                  disabled={busy !== null}
                  onClick={() => void run("retry", () => api.retryJob(job.id))}>
                  {busy === "retry" ? t("history.retrying") : t("history.retry")}
                </button>
              )
            )}

            <button
              type="button"
              className="btn ghost small quiet-danger"
              disabled={busy !== null}
              onClick={() => setConfirming(true)}>
              {t("history.delete")}
            </button>
          </>
        )}
      </div>
    </li>
  );
}

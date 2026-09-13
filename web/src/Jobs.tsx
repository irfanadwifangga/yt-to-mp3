import { useEffect, useState } from "react";
import { api, downloadFile, isTerminal, type Job } from "./api";
import { Cover } from "./Cover";
import { DownloadIcon, FolderIcon } from "./icons";
import { t } from "./i18n";
import { formatTime, messageFor, messageForCode, phaseLabel, statusLabel } from "./messages";
import { useJobStream } from "./useJobStream";

interface Props {
  jobs: Job[];
  onChanged: () => void;
}

/** Job yang masih berjalan atau menunggu giliran. */
export function ActiveList({ jobs, onChanged }: Props) {
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
            <ActiveJob key={job.id} job={job} onChanged={onChanged} />
          ))}
        </ul>
      )}
    </section>
  );
}

function ActiveJob({ job, onChanged }: { job: Job; onChanged: () => void }) {
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
          <span>{phaseLabel(phase) || statusLabel(status)}</span>
          {!waiting && (
            <span className="mono">
              {percent == null ? t("queue.calculating") : `${percent.toFixed(0)}%`}
            </span>
          )}
        </p>
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
          disabled={busy || status === "cancelling"}
        >
          {busy || status === "cancelling" ? t("queue.cancelling") : t("queue.cancel")}
        </button>
      </div>
    </li>
  );
}

/** Job yang sudah selesai, gagal, atau dibatalkan. */
export function FinishedList({ jobs, onChanged }: Props) {
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
            <FinishedJob key={job.id} job={job} onChanged={onChanged} />
          ))}
        </ul>
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
  "INVALID_URL",
]);

/** Konfirmasi hapus kembali ke keadaan semula bila diabaikan. */
const CONFIRM_TIMEOUT_MS = 5000;

type Action = "download" | "reveal" | "retry" | "delete";

function FinishedJob({ job, onChanged }: { job: Job; onChanged: () => void }) {
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

  const title = job.title || job.source_key;
  const done = job.status === "completed";
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
        </p>
        {job.error_code && job.status === "failed" && (
          <p className="item-error">{messageForCode(job.error_code)}</p>
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
          <>
            <span className="confirm-text">{t("history.confirmDelete")}</span>
            <button
              type="button"
              className="btn danger small"
              disabled={busy !== null}
              onClick={() => void run("delete", () => api.deleteJob(job.id))}
            >
              {t("history.confirmYes")}
            </button>
            <button type="button" className="btn ghost small" onClick={() => setConfirming(false)}>
              {t("history.confirmNo")}
            </button>
          </>
        ) : (
          <>
            {done && job.file_id && (
              <>
                <button
                  type="button"
                  className="btn small"
                  disabled={busy !== null}
                  onClick={() =>
                    void run("download", () => downloadFile(job.file_id!, `${title}.mp3`))
                  }
                >
                  <DownloadIcon />
                  {busy === "download" ? t("history.preparing") : t("history.download")}
                </button>
                <button
                  type="button"
                  className="btn small"
                  disabled={busy !== null}
                  onClick={() => void run("reveal", () => api.revealFile(job.file_id!))}
                >
                  <FolderIcon />
                  {t("history.reveal")}
                </button>
              </>
            )}

            {retryable && (
              <button
                type="button"
                className="btn small"
                disabled={busy !== null}
                onClick={() => void run("retry", () => api.retryJob(job.id))}
              >
                {busy === "retry" ? t("history.retrying") : t("history.retry")}
              </button>
            )}

            <button
              type="button"
              className="btn ghost small quiet-danger"
              disabled={busy !== null}
              onClick={() => setConfirming(true)}
            >
              {t("history.delete")}
            </button>
          </>
        )}
      </div>
    </li>
  );
}

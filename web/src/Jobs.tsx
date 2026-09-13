import { useState } from "react";
import { api, downloadFile, isTerminal, type Job } from "./api";
import { t } from "./i18n";
import {
  formatTime,
  messageFor,
  messageForCode,
  phaseLabel,
  statusLabel,
} from "./messages";
import { useJobStream } from "./useJobStream";

interface Props {
  jobs: Job[];
  onChanged: () => void;
}

/** Antrean: job yang masih berjalan atau menunggu. */
export function Queue({ jobs, onChanged }: Props) {
  if (jobs.length === 0) {
    return (
      <section>
        <h2>{t("queue.title")}</h2>
        <p className="muted">{t("queue.empty")}</p>
      </section>
    );
  }

  return (
    <section>
      <h2>{t("queue.title")}</h2>
      <ul className="jobs">
        {jobs.map((job) => (
          <ActiveJob key={job.id} job={job} onChanged={onChanged} />
        ))}
      </ul>
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

  return (
    <li className="job">
      <div className="job-head">
        <span className="job-title">{job.title || job.source_key}</span>
        <span className="muted">{phaseLabel(phase) || statusLabel(status)}</span>
      </div>

      <div className="bar" role="progressbar" aria-valuenow={percent ?? undefined}>
        {/* Progress null berarti indeterminate: total belum diketahui, bukan nol. */}
        <div
          className={percent == null ? "bar-fill indeterminate" : "bar-fill"}
          style={percent == null ? undefined : { width: `${percent}%` }}
        />
      </div>

      <div className="job-foot">
        <span className="muted">
          {percent == null ? t("queue.calculating") : `${percent.toFixed(0)}%`}
        </span>
        <button type="button" onClick={() => void handleCancel()} disabled={busy}>
          {busy ? t("queue.cancelling") : t("queue.cancel")}
        </button>
      </div>

      {error && <p className="error">{error}</p>}
    </li>
  );
}

/** Riwayat: job yang sudah selesai, gagal, atau dibatalkan. */
export function History({ jobs, onChanged }: Props) {
  if (jobs.length === 0) {
    return (
      <section>
        <h2>{t("history.title")}</h2>
        <p className="muted">{t("history.empty")}</p>
      </section>
    );
  }

  return (
    <section>
      <h2>{t("history.title")}</h2>
      <ul className="jobs">
        {jobs.map((job) => (
          <HistoryRow key={job.id} job={job} onChanged={onChanged} />
        ))}
      </ul>
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

type Action = "download" | "reveal" | "retry" | "delete";

function HistoryRow({ job, onChanged }: { job: Job; onChanged: () => void }) {
  const [busy, setBusy] = useState<Action | null>(null);
  const [error, setError] = useState<string | null>(null);

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
    }
  }

  const done = job.status === "completed";
  const filename = `${job.title || job.source_key}.mp3`;
  const retryable = job.status !== "completed" && !PERMANENT.has(job.error_code ?? "");

  return (
    <li className="job">
      <div className="job-head">
        <span className="job-title">{job.title || job.source_key}</span>
        <span className={done ? "tag ok" : "tag off"}>{statusLabel(job.status)}</span>
      </div>

      <div className="muted small">
        {formatTime(job.finished_at ?? job.created_at)}
        {job.error_code && ` — ${messageForCode(job.error_code)}`}
      </div>

      <div className="actions">
        {done && job.file_id && (
          <>
            <button
              type="button"
              disabled={busy !== null}
              onClick={() => void run("download", () => downloadFile(job.file_id!, filename))}
            >
              {busy === "download" ? t("history.preparing") : t("history.download")}
            </button>
            <button
              type="button"
              disabled={busy !== null}
              onClick={() => void run("reveal", () => api.revealFile(job.file_id!))}
            >
              {t("history.reveal")}
            </button>
          </>
        )}

        {done && !job.file_id && <span className="muted small">{t("history.missing")}</span>}

        {retryable && (
          <button
            type="button"
            disabled={busy !== null}
            onClick={() => void run("retry", () => api.retryJob(job.id))}
          >
            {busy === "retry" ? t("history.retrying") : t("history.retry")}
          </button>
        )}

        <button
          type="button"
          className="danger"
          disabled={busy !== null}
          onClick={() => void run("delete", () => api.deleteJob(job.id))}
        >
          {t("history.delete")}
        </button>
      </div>

      {error && <p className="error">{error}</p>}
    </li>
  );
}

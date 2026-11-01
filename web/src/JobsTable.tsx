import { Select } from "./Select";
import { useCallback, useEffect, useRef, useState } from "react";
import { api, downloadFile, isTerminal, updateYtdlpAndRetry, type Job, type Preset } from "./api";
import { Cover } from "./Cover";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  CloseIcon,
  DownloadIcon,
  FolderIcon,
  RetryIcon,
  SearchIcon,
  TrashIcon,
} from "./icons";
import { t } from "./i18n";
import {
  formatBytes,
  formatDuration,
  messageFor,
  messageForCode,
  presetFullLabel,
  statusLabel,
} from "./messages";
import { useJobStream } from "./useJobStream";
import { estimateConstantBytes } from "./estimate";

const STATUSES = [
  "active",
  "queued",
  "downloading",
  "converting",
  "completed",
  "failed",
  "cancelled",
];
const PERMANENT = new Set([
  "VIDEO_PRIVATE",
  "VIDEO_UNAVAILABLE",
  "GEO_BLOCKED",
  "AGE_RESTRICTED",
  "LIVE_NOT_SUPPORTED",
  "UNSUPPORTED_URL",
  "INVALID_URL",
  "INVALID_TRIM",
]);

export function JobsTable({ presets, refreshKey = 0 }: { presets: Preset[]; refreshKey?: number }) {
  const [rows, setRows] = useState<Job[]>([]);
  const rowsRef = useRef<Job[]>([]);
  const [search, setSearch] = useState("");
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [kind, setKind] = useState("");
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [updates, setUpdates] = useState(false);
  const [revision, setRevision] = useState(0);
  const changed = useCallback(() => setRevision((n) => n + 1), []);
  useEffect(() => {
    const timer = setTimeout(() => {
      setQuery(search.trim());
      setPage(1);
    }, 300);
    return () => clearTimeout(timer);
  }, [search]);
  useEffect(() => {
    let disposed = false;
    let request: AbortController | undefined;
    async function load(replace: boolean) {
      if (request) return;
      const controller = new AbortController();
      request = controller;
      if (replace) setLoading(true);
      try {
        const result = await api.jobsPage(
          { page, page_size: pageSize, q: query, status, kind },
          controller.signal,
        );
        if (disposed) return;
        setTotal(result.total);
        if (result.page !== page) {
          setPage(result.page);
          return;
        }
        const idsChanged =
          result.jobs.map((j) => j.id).join() !== rowsRef.current.map((j) => j.id).join();
        if (replace || !idsChanged) {
          rowsRef.current = result.jobs;
          setRows(result.jobs);
          setUpdates(false);
        } else {
          // New tasks should not move the row a user is reading or about to act on.
          const fresh = new Map(result.jobs.map((j) => [j.id, j]));
          rowsRef.current = rowsRef.current.map((j) => fresh.get(j.id) ?? j);
          setRows(rowsRef.current);
          setUpdates(true);
        }
        setError("");
      } catch (err) {
        if (!disposed) setError(messageFor(err));
      } finally {
        request = undefined;
        if (!disposed) setLoading(false);
      }
    }
    function resume() {
      if (document.visibilityState === "hidden") return;
      request?.abort();
      // The next tick retries after the aborted request has released its slot.
      setRevision((n) => n + 1);
    }
    void load(true);
    const timer = setInterval(() => void load(false), 3000);
    window.addEventListener("focus", resume);
    window.addEventListener("online", resume);
    return () => {
      disposed = true;
      clearInterval(timer);
      request?.abort();
      window.removeEventListener("focus", resume);
      window.removeEventListener("online", resume);
    };
  }, [page, pageSize, query, status, kind, refreshKey, revision]);
  const pages = Math.max(1, Math.ceil(total / pageSize));
  const firstPage = Math.max(1, Math.min(page - 2, pages - 4));
  const pageNumbers = Array.from({ length: Math.min(5, pages) }, (_, i) => firstPage + i);
  return (
    <section className="queue-workspace" aria-labelledby="jobs-heading">
      <div className="queue-toolbar">
        <h2 id="jobs-heading">
          {t("jobs.title")} <span className="count">{total}</span>
        </h2>
        <form
          className="queue-search"
          role="search"
          aria-label={t("jobs.search")}
          onSubmit={(e) => {
            e.preventDefault();
            setQuery(search.trim());
            setPage(1);
          }}>
          <button type="submit" className="queue-search-submit" aria-label={t("jobs.search")}>
            <SearchIcon />
          </button>
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("jobs.search")}
            aria-label={t("jobs.search")}
            type="search"
            maxLength={200}
          />
          {search && (
            <button
              type="button"
              className="icon-btn"
              aria-label={t("capture.clearInput")}
              onClick={() => {
                setSearch("");
                setQuery("");
                setPage(1);
              }}>
              <CloseIcon />
            </button>
          )}
        </form>
        <Select
          value={status}
          aria-label={t("jobs.status")}
          onChange={(e) => {
            setStatus(e.target.value);
            setPage(1);
          }}>
          <option value="">{t("jobs.allStatuses")}</option>
          {STATUSES.map((s) => (
            <option key={s} value={s}>
              {s === "active" ? t("queue.title") : statusLabel(s)}
            </option>
          ))}
        </Select>
        <Select
          value={kind}
          aria-label={t("capture.kind")}
          onChange={(e) => {
            setKind(e.target.value);
            setPage(1);
          }}>
          <option value="">{t("jobs.allKinds")}</option>
          <option value="audio">{t("capture.kindAudio")}</option>
          <option value="video">{t("capture.kindVideo")}</option>
        </Select>
      </div>
      {error && (
        <p className="alert" role="alert">
          {error}{" "}
          <button type="button" className="link" onClick={changed}>
            {t("capture.retry")}
          </button>
        </p>
      )}
      {updates && (
        <button className="list-update" type="button" onClick={changed}>
          {t("jobs.updated")}
        </button>
      )}
      <div className="queue-table-scroll" aria-busy={loading}>
        <table className="queue-table">
          <thead>
            <tr>
              <th>{t("capture.tagTitle")}</th>
              <th>{t("capture.format")}</th>
              <th>{t("jobs.duration")}</th>
              <th>{t("jobs.size")}</th>
              <th>{t("jobs.status")}</th>
              <th>{t("jobs.actions")}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((job) => (
              <JobRow
                key={job.id}
                job={job}
                preset={presets.find((p) => p.id === job.preset_id)}
                onChanged={changed}
              />
            ))}
          </tbody>
        </table>
        {rows.length === 0 && (
          <p className="empty" role="status">
            {loading
              ? t("jobs.loading")
              : query || status || kind
                ? t("jobs.noMatch")
                : t("queue.empty")}
          </p>
        )}
      </div>
      <footer className="queue-pagination">
        <span className="hint">
          {t("jobs.range", {
            start: total === 0 ? 0 : (page - 1) * pageSize + 1,
            end: Math.min(page * pageSize, total),
            total,
          })}
        </span>
        <label>
          {t("jobs.perPage")}{" "}
          <Select
            value={pageSize}
            onChange={(e) => {
              setPageSize(Number(e.target.value));
              setPage(1);
            }}>
            {[10, 25, 50].map((n) => (
              <option key={n}>{n}</option>
            ))}
          </Select>
        </label>
        <nav aria-label={t("jobs.pagination")}>
          <button
            type="button"
            className="icon-btn"
            aria-label={t("jobs.previous")}
            disabled={page <= 1 || loading}
            onClick={() => setPage((n) => n - 1)}>
            <ChevronLeftIcon />
          </button>
          {pageNumbers.map((n) => (
            <button
              type="button"
              key={n}
              className="page-button"
              aria-current={n === page ? "page" : undefined}
              onClick={() => setPage(n)}
              disabled={loading}>
              {n}
            </button>
          ))}
          <button
            type="button"
            className="icon-btn"
            aria-label={t("jobs.next")}
            disabled={page >= pages || loading}
            onClick={() => setPage((n) => n + 1)}>
            <ChevronRightIcon />
          </button>
        </nav>
      </footer>
    </section>
  );
}

function JobRow({ job, preset, onChanged }: { job: Job; preset?: Preset; onChanged: () => void }) {
  const stream = useJobStream(job.id, !isTerminal(job.status));
  const status = isTerminal(job.status) ? job.status : (stream.status ?? job.status);
  const progress = isTerminal(job.status) ? job.progress : (stream.percent ?? job.progress);
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const [confirm, setConfirm] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    if (stream.done) onChanged();
  }, [stream.done, onChanged]);
  useEffect(() => {
    setConfirm(false);
  }, [job.id]);
  async function run(action: () => Promise<unknown>) {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    setError("");
    try {
      await action();
      onChanged();
    } catch (err) {
      setError(messageFor(err));
    } finally {
      busyRef.current = false;
      setBusy(false);
      setConfirm(false);
    }
  }
  const title = job.title || job.source_key;
  const hasFile = status === "completed" && Boolean(job.file_id);
  const estimatedSize =
    preset && job.duration_ms ? estimateConstantBytes(preset, job.duration_ms) : null;
  return (
    <tr>
      <td>
        <div className="queue-title">
          <Cover sourceKey={job.source_key} state={status === "completed" ? "done" : "dim"} />
          <div>
            <strong title={title}>{title}</strong>
            {job.file_name && <span className="hint">{job.file_name}</span>}
            {error && (
              <span className="item-error" role="alert">
                {error}
              </span>
            )}
            {job.error_code && <span className="item-error">{messageForCode(job.error_code)}</span>}
            {status === "completed" && !hasFile && (
              <span className="hint">{t("history.missing")}</span>
            )}
          </div>
        </div>
      </td>
      <td>{preset ? presetFullLabel(preset) : job.preset_id}</td>
      <td className="mono">{job.duration_ms ? formatDuration(job.duration_ms) : "—"}</td>
      <td className="mono" title={job.file_size_bytes ? undefined : t("estimate.hint")}>
        {job.file_size_bytes
          ? formatBytes(job.file_size_bytes)
          : estimatedSize !== null
            ? `≈ ${formatBytes(estimatedSize)}`
            : "—"}
      </td>
      <td>
        <span className={`job-status status-${status}`}>{statusLabel(status)}</span>
        {!isTerminal(status) && status !== "queued" && (
          <progress
            max={100}
            value={progress ?? undefined}
            aria-label={t("queue.progressLabel", { title })}
          />
        )}
        {job.retry_at && status === "queued" && (
          <span className="hint">
            {t("queue.retryScheduled", {
              seconds: Math.max(0, Math.ceil((Date.parse(job.retry_at) - Date.now()) / 1000)),
              attempt: job.attempt_count,
              max: 3,
            })}
          </span>
        )}
      </td>
      <td>
        <div className="queue-actions">
          {!isTerminal(status) ? (
            <button
              type="button"
              className="btn small ghost"
              disabled={busy || status === "cancelling"}
              onClick={() => void run(() => api.cancelJob(job.id))}>
              <CloseIcon />
              {t("queue.cancel")}
            </button>
          ) : confirm ? (
            <>
              <span>{t("history.confirmDelete")}</span>
              <button
                type="button"
                className="btn small"
                disabled={busy}
                onClick={() => void run(() => api.deleteJob(job.id))}>
                {t("history.removeOnly")}
              </button>
              {hasFile && (
                <button
                  type="button"
                  className="btn danger small"
                  disabled={busy}
                  onClick={() => void run(() => api.deleteJob(job.id, true))}>
                  {t("history.removeWithFile")}
                </button>
              )}
              <button
                type="button"
                className="btn small"
                disabled={busy}
                onClick={() => setConfirm(false)}>
                {t("history.confirmNo")}
              </button>
            </>
          ) : (
            <>
              {hasFile && (
                <>
                  <button
                    type="button"
                    className="btn small"
                    disabled={busy}
                    onClick={() => void run(() => api.revealFile(job.file_id!))}>
                    <FolderIcon />
                    {t("history.reveal")}
                  </button>
                  <button
                    type="button"
                    className="icon-btn"
                    disabled={busy}
                    title={t("history.saveCopy")}
                    aria-label={t("history.saveCopy")}
                    onClick={() =>
                      void run(() =>
                        downloadFile(
                          job.file_id!,
                          job.file_name || `${title}.${preset?.format ?? "mp3"}`,
                        ),
                      )
                    }>
                    <DownloadIcon />
                  </button>
                </>
              )}
              {status !== "completed" && !PERMANENT.has(job.error_code ?? "") && (
                <button
                  type="button"
                  className="btn small"
                  disabled={busy}
                  onClick={() =>
                    void run(() =>
                      job.error_code === "TOOL_OUTDATED"
                        ? updateYtdlpAndRetry(job.id)
                        : api.retryJob(job.id),
                    )
                  }>
                  <RetryIcon />
                  {t(
                    job.error_code === "TOOL_OUTDATED" ? "history.updateAndRetry" : "history.retry",
                  )}
                </button>
              )}
              <button
                type="button"
                className="icon-btn quiet-danger"
                disabled={busy}
                title={t("history.delete")}
                aria-label={t("history.delete")}
                onClick={() => setConfirm(true)}>
                <TrashIcon />
              </button>
            </>
          )}
        </div>
      </td>
    </tr>
  );
}

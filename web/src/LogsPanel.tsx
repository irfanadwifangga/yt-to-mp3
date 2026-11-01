import { Select } from "./Select";
import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { FolderIcon } from "./icons";
import { t } from "./i18n";
import { messageFor } from "./messages";

export function LogsPanel() {
  const [lines, setLines] = useState<string[]>([]);
  const [logPath, setLogPath] = useState<string>("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filterLevel, setFilterLevel] = useState<string>("ALL");
  const [search, setSearch] = useState("");
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [copied, setCopied] = useState(false);
  const [revealing, setRevealing] = useState(false);
  const [userScrolled, setUserScrolled] = useState(false);
  const logEndRef = useRef<HTMLDivElement>(null);
  const consoleRef = useRef<HTMLDivElement>(null);

  const request = useRef<AbortController | null>(null);
  async function fetchLogs(silent = false) {
    if (request.current) return;
    const controller = new AbortController();
    request.current = controller;
    if (!silent) setLoading(true);
    try {
      const res = await api.logs(500, controller.signal);
      if (controller.signal.aborted) return;
      setLines(res.lines);
      setLogPath(res.path);
      setError(null);
    } catch (err) {
      if (!controller.signal.aborted) setError(messageFor(err, t("logs.loadFailed")));
    } finally {
      if (request.current === controller) request.current = null;
      if (!controller.signal.aborted && !silent) setLoading(false);
    }
  }

  useEffect(() => {
    void fetchLogs();
    return () => {
      request.current?.abort();
      request.current = null;
    };
  }, []);

  useEffect(() => {
    if (!autoRefresh) return;
    const timer = setInterval(() => {
      void fetchLogs(true);
    }, 3000);
    return () => clearInterval(timer);
  }, [autoRefresh]);

  useEffect(() => {
    if (!userScrolled && logEndRef.current) {
      logEndRef.current.scrollIntoView?.({ behavior: "instant" });
    }
  }, [lines, userScrolled]);

  const filtered = lines.filter((line) => {
    if (filterLevel !== "ALL") {
      const target = `level=${filterLevel}`;
      if (!line.toUpperCase().includes(target)) return false;
    }
    if (search.trim()) {
      if (!line.toLowerCase().includes(search.toLowerCase())) return false;
    }
    return true;
  });

  async function handleCopy() {
    const text = filtered.join("\n");
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setError(t("logs.copyFailed"));
    }
  }

  async function handleReveal() {
    setRevealing(true);
    try {
      await api.revealLogs();
    } catch (err) {
      setError(messageFor(err, t("logs.revealFailed")));
    } finally {
      setRevealing(false);
    }
  }

  return (
    <div className="logs-panel">
      {error && (
        <p className="alert" role="alert">
          {error}
        </p>
      )}

      <div className="logs-toolbar">
        <div className="logs-filters">
          <Select
            value={filterLevel}
            onChange={(e) => setFilterLevel(e.target.value)}
            aria-label={t("logs.filterAll")}>
            <option value="ALL">{t("logs.filterAll")}</option>
            <option value="INFO">INFO</option>
            <option value="WARN">WARN</option>
            <option value="ERROR">ERROR</option>
            <option value="DEBUG">DEBUG</option>
          </Select>

          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t("logs.searchPlaceholder")}
            className="logs-search-input"
          />
        </div>

        <div className="logs-actions">
          <label className="logs-auto-refresh">
            <input
              type="checkbox"
              checked={autoRefresh}
              onChange={(e) => setAutoRefresh(e.target.checked)}
            />
            <span>{t("logs.autoRefresh")}</span>
          </label>

          <button
            type="button"
            className="btn small"
            onClick={() => void fetchLogs()}
            disabled={loading}>
            {t("logs.refresh")}
          </button>

          <button
            type="button"
            className="btn small"
            onClick={() => void handleCopy()}
            disabled={filtered.length === 0}>
            {copied ? t("logs.copied") : t("logs.copy")}
          </button>

          <button
            type="button"
            className="btn small"
            onClick={() => void handleReveal()}
            disabled={revealing}
            title={logPath}>
            <FolderIcon />
            {t("logs.openFile")}
          </button>
        </div>
      </div>

      <div
        className="logs-console"
        ref={consoleRef}
        onScroll={(e) => {
          const el = e.currentTarget;
          const isNearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;
          setUserScrolled(!isNearBottom);
        }}>
        {lines.length === 0 ? (
          <p className="logs-empty">{t("logs.empty")}</p>
        ) : filtered.length === 0 ? (
          <p className="logs-empty">{t("logs.noMatch")}</p>
        ) : (
          filtered.map((line, idx) => {
            const isError = line.includes("level=ERROR") || line.includes("level=error");
            const isWarn = line.includes("level=WARN") || line.includes("level=warn");
            const isInfo = line.includes("level=INFO") || line.includes("level=info");
            const isDebug = line.includes("level=DEBUG") || line.includes("level=debug");
            const levelClass = isError
              ? "log-error"
              : isWarn
                ? "log-warn"
                : isInfo
                  ? "log-info"
                  : isDebug
                    ? "log-debug"
                    : "";
            return (
              <div key={idx} className={`logs-line ${levelClass}`}>
                <span className="logs-line-num">{idx + 1}</span>
                <span className="logs-line-text">{line}</span>
              </div>
            );
          })
        )}
        <div ref={logEndRef} />
      </div>

      <div className="logs-foot">
        <span className="hint">
          {t("logs.summary", { count: filtered.length, total: lines.length })}
        </span>
        {logPath && (
          <code className="logs-path" title={logPath}>
            {logPath}
          </code>
        )}
      </div>
    </div>
  );
}

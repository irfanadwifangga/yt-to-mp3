import { useEffect, useRef, useState } from "react";
import { api, ApiError, type LocalPreviewStatus, type Metadata } from "./api";
import { t } from "./i18n";
import { formatDuration, messageFor } from "./messages";
import { PlayIcon } from "./icons";

function delay(signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", abort);
      resolve();
    }, 1000);
    function abort() {
      clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    }
    signal.addEventListener("abort", abort, { once: true });
    if (signal.aborted) abort();
  });
}
export function LocalPreview({
  media,
  startMS,
  endMS,
  trimEnabled,
  onBack,
}: {
  media: Metadata;
  startMS: number;
  endMS: number | null;
  trimEnabled: boolean;
  onBack: () => void;
}) {
  const [status, setStatus] = useState<LocalPreviewStatus | null>(null),
    [src, setSrc] = useState(""),
    [error, setError] = useState(""),
    [fetching, setFetching] = useState(false),
    [attempt, setAttempt] = useState(0);
  const video = useRef<HTMLVideoElement>(null);
  const mountedVideo = useRef<HTMLVideoElement | null>(null);
  const bounds = useRef({ startMS, endMS, trimEnabled });
  bounds.current = { startMS, endMS, trimEnabled };
  const sourceURL = `https://www.youtube.com/watch?v=${media.source_key.split(":")[1]}`;
  useEffect(() => {
    let disposed = false,
      id: string | undefined,
      objectURL: string | undefined;
    const controller = new AbortController();
    setStatus(null);
    setSrc("");
    setError("");
    setFetching(false);
    void (async () => {
      try {
        await Promise.resolve();
        if (disposed) return;
        const created = await api.createPreview(sourceURL);
        id = created.id;
        if (disposed) {
          void api.deletePreview(id).catch(() => {});
          return;
        }
        let current = created;
        setStatus(current);
        while (current.state === "preparing") {
          await delay(controller.signal);
          current = await api.previewStatus(id, controller.signal);
          if (disposed) return;
          setStatus(current);
        }
        if (current.state !== "ready")
          throw new ApiError(current.error_code ?? "DOWNLOAD_FAILED", 422);
        setFetching(true);
        const blob = await api.previewMedia(id, controller.signal);
        if (disposed) return;
        objectURL = URL.createObjectURL(blob);
        setSrc(objectURL);
        setFetching(false);
      } catch (err) {
        if (!disposed && !controller.signal.aborted) {
          setFetching(false);
          setError(messageFor(err));
        }
      }
    })();
    return () => {
      disposed = true;
      controller.abort();
      const node = mountedVideo.current;
      if (node) {
        node.pause();
        node.removeAttribute("src");
        node.load();
      }
      mountedVideo.current = null;
      if (objectURL) URL.revokeObjectURL(objectURL);
      if (id) void api.deletePreview(id).catch(() => {});
    };
  }, [sourceURL, attempt]);
  function seekStart() {
    const node = video.current;
    if (!node || !Number.isFinite(node.duration)) return;
    node.currentTime = Math.max(
      0,
      Math.min(bounds.current.trimEnabled ? bounds.current.startMS / 1000 : 0, node.duration),
    );
  }
  useEffect(() => {
    if (src && video.current?.readyState) {
      video.current.pause();
      seekStart();
    }
  }, [src, startMS, trimEnabled]);
  function guard() {
    const node = video.current;
    const b = bounds.current;
    if (!node || !b.trimEnabled || b.endMS === null || node.paused) return;
    if (node.currentTime * 1000 >= b.endMS) {
      node.pause();
      node.currentTime = b.endMS / 1000;
    } else if (node.currentTime * 1000 < b.startMS) node.currentTime = b.startMS / 1000;
  }
  useEffect(() => {
    if (!src) return;
    const timer = setInterval(guard, 50);
    return () => clearInterval(timer);
  }, [src]);
  const ready = Boolean(src);
  return (
    <div className={`source-preview local-preview ${ready ? "is-ready" : ""}`}>
      <div className="source-player">
        {ready ? (
          <video
            ref={(node) => {
              video.current = node;
              if (node) mountedVideo.current = node;
            }}
            src={src}
            controls
            playsInline
            preload="metadata"
            aria-label={t("preview.localPlayer")}
            onLoadedMetadata={seekStart}
            onTimeUpdate={guard}
            onPlay={guard}
            onError={() => setError(t("preview.localPlaybackFailed"))}
          />
        ) : (
          <img
            className="source-poster"
            src={`https://i.ytimg.com/vi/${media.source_key.split(":")[1]}/hqdefault.jpg`}
            alt=""
          />
        )}
      </div>
      {error ? (
        <p className="hint warn" role="alert">
          {error}{" "}
          <button className="btn small" type="button" onClick={() => setAttempt((n) => n + 1)}>
            {t("capture.retry")}
          </button>
        </p>
      ) : !ready ? (
        <div className="local-preview-progress" role="status">
          <span>
            {fetching
              ? t("preview.localFetching")
              : status?.phase === "encoding"
                ? t("preview.localEncoding")
                : t("preview.localDownloading")}
            {!fetching && status?.percent !== undefined ? ` ${Math.round(status.percent)}%` : ""}
          </span>
          <progress
            aria-label={t("preview.localProgress")}
            max={100}
            value={fetching ? undefined : status?.percent}
          />
        </div>
      ) : (
        <p className="hint">{t("preview.localHint")}</p>
      )}
      <div className="local-preview-actions">
        {ready && trimEnabled && endMS !== null && (
          <button
            className="btn small"
            type="button"
            onClick={() => {
              seekStart();
              void video.current?.play().catch(() => setError(t("preview.localPlaybackFailed")));
            }}>
            <PlayIcon />
            {t("preview.playSelection")}
          </button>
        )}
        <button className="btn small ghost" type="button" onClick={onBack}>
          {ready || error ? t("preview.localBack") : t("queue.cancel")}
        </button>
        {ready && trimEnabled && endMS !== null && (
          <span className="hint">
            {t("preview.range", { start: formatDuration(startMS), end: formatDuration(endMS) })}
          </span>
        )}
      </div>
    </div>
  );
}

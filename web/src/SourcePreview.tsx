import { useEffect, useRef, useState } from "react";
import type { Metadata } from "./api";
import { t } from "./i18n";
import { PlayIcon } from "./icons";
import { formatDuration } from "./messages";

interface Player {
  mute(): void;
  unMute(): void;
  playVideo(): void;
  pauseVideo(): void;
  seekTo(seconds: number, allowSeekAhead: boolean): void;
  getCurrentTime(): number;
  destroy(): void;
}
interface PlayerAPI {
  Player: new (
    element: HTMLElement,
    options: {
      events: { onReady(): void; onError(): void; onStateChange(event: { data: number }): void };
    },
  ) => Player;
}
declare global {
  interface Window {
    YT?: PlayerAPI;
    onYouTubeIframeAPIReady?: () => void;
  }
}
let loading: Promise<PlayerAPI> | undefined;
function loadPlayer(): Promise<PlayerAPI> {
  if (window.YT?.Player) return Promise.resolve(window.YT);
  if (loading) return loading;
  loading = new Promise<PlayerAPI>((resolve, reject) => {
    const script = document.createElement("script");
    const timer = setTimeout(fail, 15000);
    function fail() {
      clearTimeout(timer);
      script.remove();
      loading = undefined;
      reject(new Error("player unavailable"));
    }
    window.onYouTubeIframeAPIReady = () => {
      clearTimeout(timer);
      if (window.YT) resolve(window.YT);
      else fail();
    };
    script.src = "https://www.youtube.com/iframe_api";
    script.referrerPolicy = "strict-origin-when-cross-origin";
    script.onerror = fail;
    document.head.append(script);
  });
  return loading;
}

export function SourcePreview({
  media,
  startMS,
  endMS,
  trimEnabled = false,
}: {
  media: Metadata;
  startMS: number;
  endMS: number | null;
  trimEnabled?: boolean;
}) {
  const host = useRef<HTMLDivElement>(null);
  const player = useRef<Player | null>(null);
  const range = useRef({ startMS, endMS });
  range.current = { startMS, endMS };
  const [playing, setPlaying] = useState(false);
  const [failed, setFailed] = useState(false);
  const [ready, setReady] = useState(false);
  const [frameReady, setFrameReady] = useState(false);
  const preparingFrame = useRef(false);
  const requestedPlay = useRef(false);
  const previewedStart = useRef(startMS);
  useEffect(() => {
    if (trimEnabled) setPlaying(true);
  }, [trimEnabled]);
  useEffect(() => {
    if (!ready || !trimEnabled) {
      previewedStart.current = startMS;
      return;
    }
    if (previewedStart.current === startMS) return;
    const timer = setTimeout(() => {
      previewedStart.current = startMS;
      player.current?.pauseVideo();
      player.current?.seekTo(startMS / 1000, true);
    }, 180);
    return () => clearTimeout(timer);
  }, [startMS, trimEnabled, ready]);
  const videoID = /^youtube:([\w-]{11})$/.exec(media.source_key)?.[1];
  useEffect(() => {
    if (!playing || !videoID || !host.current) return;
    let disposed = false;
    const readyTimeout = setTimeout(() => {
      if (!disposed) setFailed(true);
    }, 20000);
    const container = host.current;
    const iframe = document.createElement("iframe");
    iframe.title = media.title;
    iframe.referrerPolicy = "strict-origin-when-cross-origin";
    iframe.allow = "autoplay; encrypted-media; picture-in-picture; fullscreen";
    const params = new URLSearchParams({
      enablejsapi: "1",
      origin: location.origin,
      playsinline: "1",
    });
    iframe.src = `https://www.youtube-nocookie.com/embed/${videoID}?${params}`;
    container.replaceChildren(iframe);
    void loadPlayer()
      .then((api) => {
        if (disposed) return;
        player.current = new api.Player(iframe, {
          events: {
            onReady() {
              if (disposed) return;
              clearTimeout(readyTimeout);
              setReady(true);
              player.current?.seekTo(range.current.startMS / 1000, true);
              if (requestedPlay.current) {
                player.current?.unMute();
                player.current?.playVideo();
              } else {
                preparingFrame.current = true;
                player.current?.mute();
                player.current?.playVideo();
              }
            },
            onStateChange(event) {
              if (disposed || event.data !== 1) return;
              setFrameReady(true);
              if (preparingFrame.current && !requestedPlay.current) {
                preparingFrame.current = false;
                player.current?.pauseVideo();
                player.current?.unMute();
              }
            },
            onError() {
              clearTimeout(readyTimeout);
              if (!disposed) setFailed(true);
            },
          },
        });
      })
      .catch(() => {
        if (!disposed) setFailed(true);
      });
    const timer = setInterval(() => {
      const p = player.current;
      const end = range.current.endMS;
      if (
        p &&
        end !== null &&
        typeof p.getCurrentTime === "function" &&
        p.getCurrentTime() * 1000 >= end
      )
        p.pauseVideo();
    }, 250);
    return () => {
      disposed = true;
      clearTimeout(readyTimeout);
      clearInterval(timer);
      player.current?.destroy();
      player.current = null;
      container.replaceChildren();
    };
  }, [playing, videoID, media.title]);
  return (
    <div className={`source-preview ${frameReady ? "is-ready" : ""}`}>
      <div className="source-player">
        <div ref={host} className="player-host" hidden={!playing || failed} />
        {(!frameReady || failed) && (
          <img
            className="source-poster"
            src={`https://i.ytimg.com/vi/${videoID}/hqdefault.jpg`}
            alt=""
          />
        )}
        {!playing && (
          <button
            type="button"
            className="preview-play"
            onClick={() => {
              requestedPlay.current = true;
              setPlaying(true);
            }}
            disabled={!videoID}>
            <PlayIcon />
            {t("preview.play")}
          </button>
        )}
      </div>
      {failed ? (
        <p className="hint warn" role="status">
          {t("preview.failed")}{" "}
          <a
            href={`https://www.youtube.com/watch?v=${videoID}`}
            target="_blank"
            rel="noopener noreferrer">
            {t("preview.openSource")}
          </a>
        </p>
      ) : playing && !ready ? (
        <p className="hint" role="status">
          {t("preview.loading")}
        </p>
      ) : (
        <p className="hint">{t("preview.sourceHint")}</p>
      )}
      {trimEnabled && !failed && endMS !== null && (
        <div className="trim-preview-bar">
          <span>
            {t("preview.range", { start: formatDuration(startMS), end: formatDuration(endMS) })}
          </span>
          <button
            className="btn small"
            type="button"
            onClick={() => {
              requestedPlay.current = true;
              setPlaying(true);
              player.current?.seekTo(startMS / 1000, true);
              preparingFrame.current = false;
              player.current?.unMute();
              player.current?.playVideo();
            }}>
            <PlayIcon />
            {t("preview.playSelection")}
          </button>
        </div>
      )}
    </div>
  );
}

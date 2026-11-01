import { useEffect, useState } from "react";
import { sessionToken, isTerminal, type JobStatus } from "./api";

export interface StreamState {
  status?: JobStatus;
  phase?: string;
  percent?: number | null;
  code?: string;
  done: boolean;
}

interface Frame {
  event: string;
  data: string;
  id?: string;
}

/**
 * Mengurai aliran text/event-stream menjadi frame.
 *
 * EventSource sengaja tidak dipakai: API mewajibkan session token pada
 * header X-Session-Token, dan EventSource tidak dapat menyetel header sama
 * sekali. Menaruh token di query string akan membuatnya tertinggal di
 * riwayat dan log, yang justru dihindari model keamanan aplikasi ini.
 */
function parseFrames(chunk: string): Frame[] {
  const frames: Frame[] = [];

  for (const block of chunk.split("\n\n")) {
    if (!block.trim() || block.startsWith(":")) continue; // komentar heartbeat

    const frame: Frame = { event: "message", data: "" };
    for (const line of block.split("\n")) {
      const [field, ...rest] = line.split(":");
      const value = rest.join(":").trimStart();
      if (field === "event") frame.event = value;
      else if (field === "data") frame.data += value;
      else if (field === "id") frame.id = value;
    }
    if (frame.data) frames.push(frame);
  }
  return frames;
}

/**
 * Berlangganan progress sebuah job.
 *
 * Terputus otomatis ketika job mencapai status terminal, karena server juga
 * menutup stream pada titik itu.
 */
export function useJobStream(jobId: string | null, active: boolean): StreamState {
  const [state, setState] = useState<StreamState>({ done: false });
  useEffect(() => {
    setState({ done: false });
    if (!jobId || !active) return;
    let disposed = false;
    let finished = false;
    let generation = 0;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let controller: AbortController | undefined;
    let lastEventId: string | undefined;
    let delay = 1000;
    async function connect() {
      if (disposed || finished) return;
      const current = ++generation;
      controller?.abort();
      const next = new AbortController();
      controller = next;
      let watchdog = setTimeout(() => next.abort(), 35_000);
      setState({ done: false });
      const headers = new Headers();
      if (sessionToken) headers.set("X-Session-Token", sessionToken);
      if (lastEventId) headers.set("Last-Event-ID", lastEventId);
      try {
        const res = await fetch(`/api/jobs/${jobId}/events`, { headers, signal: next.signal });
        if (!res.ok || !res.body) throw new Error("stream unavailable");
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        try {
          while (!disposed && current === generation && !finished) {
            const { done, value } = await reader.read();
            if (done) break;
            if (current !== generation || disposed) break;
            clearTimeout(watchdog);
            watchdog = setTimeout(() => next.abort(), 35_000);
            delay = 1000;
            buffer += decoder.decode(value, { stream: true });
            const lastBreak = buffer.lastIndexOf("\n\n");
            if (lastBreak < 0) continue;
            const frames = parseFrames(buffer.slice(0, lastBreak + 2));
            buffer = buffer.slice(lastBreak + 2);
            for (const frame of frames) {
              let payload: Record<string, unknown>;
              try {
                payload = JSON.parse(frame.data);
              } catch {
                continue;
              }
              if (frame.id) lastEventId = frame.id;
              finished =
                frame.event === "done" ||
                (typeof payload.status === "string" && isTerminal(payload.status as JobStatus));
              setState((prev) => ({
                status: (payload.status as JobStatus) ?? prev.status,
                phase: (payload.phase as string) ?? prev.phase,
                percent:
                  payload.percent === undefined ? prev.percent : (payload.percent as number | null),
                code: (payload.code as string) ?? prev.code,
                done: finished,
              }));
            }
          }
        } finally {
          await reader.cancel().catch(() => {});
          reader.releaseLock();
        }
      } catch {
        /* Polling remains available while the stream reconnects. */
      } finally {
        clearTimeout(watchdog);
        if (!disposed && !finished && current === generation) {
          setState({ done: false });
          retry = setTimeout(() => void connect(), delay);
          delay = Math.min(delay * 2, 15000);
        }
      }
    }
    function resume() {
      if (document.visibilityState === "hidden" || disposed || finished) return;
      clearTimeout(retry);
      void connect();
    }
    window.addEventListener("focus", resume);
    window.addEventListener("online", resume);
    document.addEventListener("visibilitychange", resume);
    void connect();
    return () => {
      disposed = true;
      generation++;
      clearTimeout(retry);
      controller?.abort();
      window.removeEventListener("focus", resume);
      window.removeEventListener("online", resume);
      document.removeEventListener("visibilitychange", resume);
    };
  }, [jobId, active]);
  return state;
}

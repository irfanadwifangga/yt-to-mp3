import { useEffect, useRef, useState } from "react";
import { sessionToken, type JobStatus } from "./api";

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
  const lastEventId = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (!jobId || !active) return;

    const controller = new AbortController();
    let cancelled = false;

    async function listen() {
      const headers = new Headers();
      if (sessionToken) headers.set("X-Session-Token", sessionToken);
      if (lastEventId.current) headers.set("Last-Event-ID", lastEventId.current);

      try {
        const res = await fetch(`/api/jobs/${jobId}/events`, {
          headers,
          signal: controller.signal,
        });
        if (!res.ok || !res.body) return;

        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";

        while (!cancelled) {
          const { done, value } = await reader.read();
          if (done) break;

          buffer += decoder.decode(value, { stream: true });

          // Sisa setelah pemisah terakhir adalah frame yang belum utuh.
          const lastBreak = buffer.lastIndexOf("\n\n");
          if (lastBreak === -1) continue;

          const complete = buffer.slice(0, lastBreak + 2);
          buffer = buffer.slice(lastBreak + 2);

          for (const frame of parseFrames(complete)) {
            if (frame.id) lastEventId.current = frame.id;

            let payload: Record<string, unknown> = {};
            try {
              payload = JSON.parse(frame.data);
            } catch {
              continue;
            }

            setState((prev) => ({
              status: (payload.status as JobStatus) ?? prev.status,
              phase: (payload.phase as string) ?? prev.phase,
              percent:
                payload.percent === undefined
                  ? prev.percent
                  : (payload.percent as number | null),
              code: (payload.code as string) ?? prev.code,
              done: frame.event === "done",
            }));
          }
        }
      } catch {
        // Koneksi terputus atau dibatalkan; pemanggil tetap punya polling
        // sebagai cadangan sehingga tidak ada yang perlu ditangani di sini.
      }
    }

    void listen();
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [jobId, active]);

  return state;
}

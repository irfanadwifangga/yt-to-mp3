import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useJobStream } from "./useJobStream";

afterEach(() => vi.unstubAllGlobals());

it("reconnects after a lost stream and clears stale progress on resume", async () => {
  const streams: ReadableStreamDefaultController<Uint8Array>[] = [];
  const signals: AbortSignal[] = [];
  const fetcher = vi.fn((_url: string, init: RequestInit) => {
    const signal = init.signal!;
    signals.push(signal);
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        streams.push(controller);
        signal.addEventListener(
          "abort",
          () => {
            try {
              controller.error(new DOMException("aborted", "AbortError"));
            } catch {
              /* already closed */
            }
          },
          { once: true },
        );
      },
    });
    return Promise.resolve(new Response(body));
  });
  vi.stubGlobal("fetch", fetcher);
  const { result, unmount } = renderHook(() => useJobStream("job_resume", true));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
  await act(async () =>
    streams[0]!.enqueue(new TextEncoder().encode('event: progress\ndata: {"percent":25}\n\n')),
  );
  expect(result.current.percent).toBe(25);
  await act(async () => streams[0]!.close());
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2), { timeout: 2500 });
  expect(result.current.percent).toBeUndefined();
  await act(async () =>
    streams[1]!.enqueue(new TextEncoder().encode('event: progress\ndata: {"percent":60}\n\n')),
  );
  expect(result.current.percent).toBe(60);
  await act(async () => window.dispatchEvent(new Event("focus")));
  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
  expect(signals[1]!.aborted).toBe(true);
  expect(result.current.percent).toBeUndefined();
  unmount();
  expect(signals[2]!.aborted).toBe(true);
});

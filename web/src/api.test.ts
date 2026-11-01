import { afterEach, expect, it, vi } from "vitest";
import { api } from "./api";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("releases a health request that remains pending after a connection is lost", async () => {
  vi.useFakeTimers();
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (_url: string, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () =>
            reject(new DOMException("Aborted", "AbortError")),
          );
        }),
    ),
  );
  let settled = false;
  const pending = api.health().catch(() => {
    settled = true;
  });
  await vi.advanceTimersByTimeAsync(15_001);
  expect(settled).toBe(true);
  await pending;
});

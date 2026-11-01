import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import { api, ApiError, type Job, type Preset } from "./api";
import { PlaylistDialog } from "./PlaylistDialog";
import { t } from "./i18n";
vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  api: { playlist: vi.fn(), createJob: vi.fn() },
}));
vi.mock("./Modal", () => ({
  Modal: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
const presets: Preset[] = [
  {
    id: "mp4_720",
    label: "720p",
    kind: "video",
    format: "mp4",
    mode: "cbr",
    max_height: 720,
    channels: 2,
    passthrough: true,
  },
];
const entries = ["First", "Second", "Third"].map((title, i) => ({
  title,
  url: `https://www.youtube.com/watch?v=${"abc"[i]!.repeat(11)}`,
  duration_ms: 30000,
  unavailable: false,
}));
const job: Job = {
  title: "First",
  id: "job",
  source_url: entries[0]!.url,
  source_key: "youtube:aaaaaaaaaaa",
  preset_id: "mp4_720",
  status: "queued",
  filename_mode: "title",
  progress: null,
  attempt_count: 0,
  created_at: "2026-11-01T00:00:00Z",
};
function setup() {
  vi.mocked(api.playlist).mockResolvedValue({
    title: "Collection",
    entries: [...entries, { url: "", title: "Private", duration_ms: 0, unavailable: true }],
    truncated: false,
  });
  const onQueued = vi.fn();
  render(
    <PlaylistDialog
      url="https://www.youtube.com/playlist?list=PL_test"
      presets={presets}
      defaultPreset="mp4_720"
      onQueued={onQueued}
      onClose={() => {}}
    />,
  );
  return onQueued;
}
it("adds only selected available videos without applying a trim range", async () => {
  vi.mocked(api.createJob).mockResolvedValue(job);
  const onQueued = setup();
  await screen.findByText("Collection");
  expect((screen.getByRole("checkbox", { name: "Private" }) as HTMLInputElement).disabled).toBe(
    true,
  );
  fireEvent.click(screen.getByRole("checkbox", { name: "Second" }));
  fireEvent.click(screen.getByRole("checkbox", { name: "Third" }));
  fireEvent.click(screen.getByRole("button", { name: t("playlist.add", { count: 1 }) }));
  await waitFor(() => expect(onQueued).toHaveBeenCalledTimes(1));
  expect(api.createJob).toHaveBeenCalledWith(entries[0]!.url, "mp4_720");
});
it("skips active duplicates and stops when the queue is full", async () => {
  vi.mocked(api.createJob)
    .mockRejectedValueOnce(new ApiError("DUPLICATE_ACTIVE_JOB", 409))
    .mockRejectedValueOnce(new ApiError("QUEUE_FULL", 503));
  setup();
  await screen.findByText("Collection");
  fireEvent.click(screen.getByRole("button", { name: t("playlist.add", { count: 3 }) }));
  await screen.findByText(t("playlist.summary", { queued: 0, skipped: 1, failed: 1 }));
  expect(api.createJob).toHaveBeenCalledTimes(2);
});
it("stops after the current write without silently discarding queued jobs", async () => {
  let finish!: (job: Job) => void;
  vi.mocked(api.createJob).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const onQueued = setup();
  await screen.findByText("Collection");
  fireEvent.click(screen.getByRole("button", { name: t("playlist.add", { count: 3 }) }));
  await screen.findByRole("button", { name: t("playlist.stop") });
  fireEvent.click(screen.getByRole("button", { name: t("playlist.stop") }));
  await act(async () => finish(job));
  expect(api.createJob).toHaveBeenCalledTimes(1);
  expect(onQueued).toHaveBeenCalledTimes(1);
});

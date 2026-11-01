import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { api, type Health } from "./api";
import { App } from "./App";
import { t } from "./i18n";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  api: { health: vi.fn(), jobs: vi.fn(), presets: vi.fn(), settings: vi.fn(), shutdown: vi.fn() },
}));
vi.mock("./Capture", () => ({ Capture: () => null }));
vi.mock("./JobsTable", () => ({ JobsTable: () => null }));
vi.mock("./SettingsDialog", () => ({ SettingsDialog: () => null }));

it("resynchronizes on focus and discards a health response from before resume", async () => {
  const health: Health = {
    app: "yt-to-mp3",
    version: "test",
    commit: "test",
    status: "ok",
    uptime_seconds: 1,
    spa_built: true,
    output_dir: "D:/current",
    tools: {},
    queue: { active: 0, queued: 0, capacity: 2 },
    app_update: { current: "test", update_available: false },
  };
  let stale!: (value: Health) => void;
  vi.mocked(api.health)
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          stale = resolve;
        }),
    )
    .mockResolvedValue(health);
  vi.mocked(api.jobs).mockResolvedValue({ jobs: [] });
  vi.mocked(api.presets).mockResolvedValue({ presets: [] });
  vi.mocked(api.settings).mockResolvedValue({ settings: [] });
  render(<App />);
  await waitFor(() => expect(api.health).toHaveBeenCalledTimes(1));
  const signal = vi.mocked(api.health).mock.calls[0]![0]!;
  await act(async () => window.dispatchEvent(new Event("focus")));
  await screen.findByText("D:/current");
  expect(signal.aborted).toBe(true);
  await act(async () => stale({ ...health, output_dir: "D:/stale" }));
  expect(screen.queryByText("D:/stale")).toBeNull();
  fireEvent.click(screen.getByRole("button", { name: "Bahasa Indonesia" }));
  expect(
    screen.getByRole("button", { name: "Bahasa Indonesia" }).getAttribute("aria-pressed"),
  ).toBe("true");
  fireEvent.click(screen.getByRole("button", { name: "English" }));
  expect(screen.getByRole("button", { name: "English" }).getAttribute("aria-pressed")).toBe("true");
  const close = vi.spyOn(window, "close").mockImplementation(() => {});
  vi.mocked(api.shutdown).mockResolvedValue({ status: "ok" });
  fireEvent.click(screen.getByRole("button", { name: t("app.quit") }));
  await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
  expect(api.shutdown).toHaveBeenCalledTimes(1);
  expect(screen.queryByText(t("app.stopped.title"))).toBeNull();
  close.mockRestore();
});

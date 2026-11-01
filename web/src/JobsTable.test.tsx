import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { api, type Job } from "./api";
import { JobsTable } from "./JobsTable";
import { t } from "./i18n";

vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  api: { jobsPage: vi.fn() },
}));
vi.mock("./useJobStream", () => ({ useJobStream: () => ({ done: false }) }));
const job: Job = {
  id: "one",
  source_url: "",
  source_key: "youtube:aaaaaaaaaaa",
  title: "Recording",
  status: "cancelled",
  preset_id: "mp3",
  filename_mode: "title",
  progress: null,
  attempt_count: 0,
  created_at: "2026-10-29T00:00:00Z",
};

it("queries the server for search and numbered pages", async () => {
  vi.mocked(api.jobsPage).mockImplementation(async (params) => ({
    jobs: [job],
    total: 23,
    page: params.page,
    page_size: params.page_size,
  }));
  render(<JobsTable presets={[]} />);
  await screen.findByText("Recording");
  fireEvent.click(screen.getByRole("button", { name: t("jobs.next") }));
  await waitFor(() =>
    expect(api.jobsPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 2 }),
      expect.any(AbortSignal),
    ),
  );
  fireEvent.change(screen.getByRole("searchbox", { name: t("jobs.search") }), {
    target: { value: "old recording" },
  });
  await waitFor(() =>
    expect(api.jobsPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ q: "old recording", page: 1 }),
      expect.any(AbortSignal),
    ),
  );
  fireEvent.change(screen.getByRole("searchbox", { name: t("jobs.search") }), {
    target: { value: "another recording" },
  });
  fireEvent.submit(screen.getByRole("search", { name: t("jobs.search") }));
  await waitFor(() =>
    expect(api.jobsPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ q: "another recording", page: 1 }),
      expect.any(AbortSignal),
    ),
  );
  expect(screen.getByRole("button", { name: t("jobs.search") }).getAttribute("type")).toBe(
    "submit",
  );
  fireEvent.click(screen.getByRole("button", { name: t("capture.clearInput") }));
  await waitFor(() =>
    expect(api.jobsPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ q: "", page: 1 }),
      expect.any(AbortSignal),
    ),
  );
  expect(
    (screen.getByRole("searchbox", { name: t("jobs.search") }) as HTMLInputElement).value,
  ).toBe("");
});

it("ignores a previous filter response arriving after the current response", async () => {
  let finish!: (value: { jobs: Job[]; total: number; page: number; page_size: number }) => void;
  vi.mocked(api.jobsPage).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  vi.mocked(api.jobsPage).mockResolvedValue({
    jobs: [{ ...job, title: "Current filter" }],
    total: 1,
    page: 1,
    page_size: 10,
  });
  render(<JobsTable presets={[]} />);
  fireEvent.change(screen.getByRole("combobox", { name: t("jobs.status") }), {
    target: { value: "cancelled" },
  });
  await screen.findByText("Current filter");
  await act(async () =>
    finish({ jobs: [{ ...job, title: "Stale response" }], total: 1, page: 1, page_size: 10 }),
  );
  expect(screen.queryByText("Stale response")).toBeNull();
});

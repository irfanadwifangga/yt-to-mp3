import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, afterEach, expect, it, vi } from "vitest";
import { api, type LocalPreviewStatus, type Metadata } from "./api";
import { LocalPreview } from "./LocalPreview";
import { t } from "./i18n";
import { StrictMode } from "react";
vi.mock("./api", async (original) => ({
  ...(await original<typeof import("./api")>()),
  api: {
    createPreview: vi.fn(),
    previewStatus: vi.fn(),
    previewMedia: vi.fn(),
    deletePreview: vi.fn(),
  },
}));
const media: Metadata = {
  source_key: "youtube:aaaaaaaaaaa",
  source_url: "https://youtu.be/aaaaaaaaaaa",
  title: "Source",
  uploader: "",
  duration_ms: 10000,
  thumbnail_url: "",
  source_codec: "aac",
  sample_rate: 48000,
  video_height: 720,
  suggested_title: "Source",
  suggested_artist: "",
  previous_conversions: [],
};
beforeEach(() => {
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue();
  class TestURL extends URL {
    static createObjectURL = vi.fn(() => "blob:preview");
    static revokeObjectURL = vi.fn();
  }
  vi.stubGlobal("URL", TestURL);
  vi.mocked(api.createPreview).mockResolvedValue({ id: "local", state: "ready", phase: "ready" });
  vi.mocked(api.previewMedia).mockResolvedValue(new Blob(["preview"], { type: "video/mp4" }));
  vi.mocked(api.deletePreview).mockResolvedValue();
});
afterEach(() => {
  vi.unstubAllGlobals();
});
it("seeks to the trim start, stops at its end, and releases the temporary preview", async () => {
  const view = render(
    <StrictMode>
      <LocalPreview media={media} startMS={2500} endMS={4000} trimEnabled onBack={() => {}} />
    </StrictMode>,
  );
  const video = (await screen.findByLabelText(t("preview.localPlayer"))) as HTMLVideoElement;
  expect(api.createPreview).toHaveBeenCalledTimes(1);
  Object.defineProperty(video, "duration", { configurable: true, value: 10 });
  Object.defineProperty(video, "readyState", { configurable: true, value: 4 });
  fireEvent.loadedMetadata(video);
  expect(video.currentTime).toBe(2.5);
  fireEvent.click(screen.getByRole("button", { name: t("preview.playSelection") }));
  expect(video.play).toHaveBeenCalled();
  Object.defineProperty(video, "paused", { configurable: true, value: false });
  video.currentTime = 4.2;
  fireEvent.timeUpdate(video);
  expect(video.currentTime).toBe(4);
  expect(video.pause).toHaveBeenCalled();
  view.unmount();
  expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:preview");
  expect(api.deletePreview).toHaveBeenCalledWith("local");
});
it("cancels a preview whose creation response arrives after unmount", async () => {
  let finish!: (status: LocalPreviewStatus) => void;
  vi.mocked(api.createPreview).mockImplementationOnce(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  const view = render(
    <LocalPreview media={media} startMS={0} endMS={null} trimEnabled={false} onBack={() => {}} />,
  );
  await waitFor(() => expect(api.createPreview).toHaveBeenCalled());
  view.unmount();
  await act(async () => finish({ id: "late", state: "preparing", phase: "downloading" }));
  await waitFor(() => expect(api.deletePreview).toHaveBeenCalledWith("late"));
  expect(api.previewMedia).not.toHaveBeenCalled();
});
it("explains size failures without loading a media blob", async () => {
  vi.mocked(api.createPreview).mockResolvedValueOnce({
    id: "limit",
    state: "failed",
    phase: "downloading",
    error_code: "PREVIEW_TOO_LARGE",
  });
  render(
    <LocalPreview media={media} startMS={0} endMS={null} trimEnabled={false} onBack={() => {}} />,
  );
  await screen.findByText(t("error.PREVIEW_TOO_LARGE"));
  expect(api.previewMedia).not.toHaveBeenCalled();
});

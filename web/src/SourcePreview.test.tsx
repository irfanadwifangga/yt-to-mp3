import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { SourcePreview } from "./SourcePreview";
import type { Metadata } from "./api";
import { t } from "./i18n";

afterEach(() => {
  delete window.YT;
});
it("prepares the first frame silently, pauses, and then plays the requested range", async () => {
  const play = vi.fn(),
    pause = vi.fn(),
    seek = vi.fn(),
    mute = vi.fn(),
    unMute = vi.fn();
  class Player {
    mute = mute;
    unMute = unMute;
    playVideo() {
      play();
      queueMicrotask(() => this.events.onStateChange({ data: 1 }));
    }
    pauseVideo = pause;
    seekTo = seek;
    getCurrentTime = () => 0;
    destroy = vi.fn();
    events: { onStateChange(event: { data: number }): void };
    constructor(
      _element: HTMLElement,
      options: {
        events: { onReady(): void; onError(): void; onStateChange(event: { data: number }): void };
      },
    ) {
      this.events = options.events;
      queueMicrotask(options.events.onReady);
    }
  }
  window.YT = { Player };
  const media: Metadata = {
    source_key: "youtube:aaaaaaaaaaa",
    source_url: "",
    title: "Source",
    uploader: "",
    duration_ms: 120000,
    thumbnail_url: "",
    source_codec: "opus",
    sample_rate: 48000,
    video_height: 720,
    suggested_title: "Source",
    suggested_artist: "",
    previous_conversions: [],
  };
  const view = render(<SourcePreview media={media} startMS={24735} endMS={64140} trimEnabled />);
  await waitFor(() => expect(seek).toHaveBeenCalledWith(24.735, true));
  await waitFor(() => expect(pause).toHaveBeenCalled());
  expect(mute).toHaveBeenCalledTimes(1);
  expect(play).toHaveBeenCalledTimes(1);
  expect(mute.mock.invocationCallOrder[0]).toBeLessThan(play.mock.invocationCallOrder[0]!);
  expect(pause.mock.invocationCallOrder[0]).toBeLessThan(unMute.mock.invocationCallOrder[0]!);
  fireEvent.click(screen.getByRole("button", { name: t("preview.playSelection") }));
  expect(play).toHaveBeenCalledTimes(2);
  view.rerender(<SourcePreview media={media} startMS={30000} endMS={64140} trimEnabled />);
  await waitFor(() => expect(seek).toHaveBeenLastCalledWith(30, true));
});

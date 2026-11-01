import { expect, it } from "vitest";
import { estimateBytes } from "./estimate";
import type { Metadata, Preset } from "./api";

const media: Metadata = {
  source_key: "youtube:aaaaaaaaaaa",
  source_url: "",
  title: "Test",
  uploader: "Test",
  duration_ms: 180000,
  thumbnail_url: "",
  source_codec: "opus",
  sample_rate: 48000,
  video_height: 1080,
  suggested_title: "Test",
  suggested_artist: "Test",
  previous_conversions: [],
};
const preset: Preset = {
  id: "mp3_standard",
  label: "MP3",
  kind: "audio",
  format: "mp3",
  mode: "cbr",
  bitrate_kbps: 192,
  channels: 2,
  passthrough: false,
};
it("uses the selected duration for CBR output", () => {
  expect(estimateBytes(media, preset, 90000, true)).toBe(2160000);
});
it("derives PCM size from sample rate and channels", () => {
  expect(estimateBytes(media, { ...preset, format: "wav", mode: "lossless" }, 1000, false)).toBe(
    192000,
  );
});
it("leaves unknown variable bitrate output unknown", () => {
  expect(estimateBytes(media, { ...preset, mode: "vbr" }, 90000, false)).toBeNull();
});
it("uses source stream metadata for untrimmed Opus", () => {
  const source = {
    ...media,
    source_formats: [
      { height: 0, video_codec: "none", audio_codec: "opus", bitrate_kbps: 160, size_bytes: 0 },
    ],
  };
  const opus = {
    ...preset,
    format: "opus",
    mode: "source",
    passthrough: true,
    bitrate_kbps: undefined,
  };
  expect(estimateBytes(source, opus, 1000, false)).toBe(20000);
  expect(estimateBytes(source, opus, 1000, true)).toBeNull();
});

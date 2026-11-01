import type { Metadata, Preset, SourceFormat } from "./api";

export function estimateConstantBytes(
  preset: Preset,
  durationMS: number,
  sourceSampleRate?: number,
): number | null {
  if (durationMS <= 0) return null;
  const seconds = durationMS / 1000;
  if (preset.kind === "audio" && preset.mode === "cbr" && preset.bitrate_kbps) {
    return Math.round((seconds * preset.bitrate_kbps * 1000) / 8);
  }
  if (preset.format === "wav") {
    const rate = preset.sample_rate || sourceSampleRate;
    if (!rate || !preset.channels) return null;
    return Math.round(seconds * rate * preset.channels * 2);
  }
  return null;
}

export function estimateBytes(
  media: Metadata,
  preset: Preset,
  durationMS: number,
  trimmed: boolean,
): number | null {
  if (durationMS <= 0) return null;
  const constant = estimateConstantBytes(preset, durationMS, media.sample_rate);
  if (constant !== null) return constant;
  const seconds = durationMS / 1000;
  if (!preset.passthrough || trimmed) return null;
  const formats = media.source_formats ?? [];
  const size = (f: SourceFormat) =>
    f.size_bytes > 0 && media.duration_ms > 0
      ? (f.size_bytes * durationMS) / media.duration_ms
      : f.bitrate_kbps > 0
        ? (seconds * f.bitrate_kbps * 1000) / 8
        : 0;
  const audio = formats.filter(
    (f) => (!f.video_codec || f.video_codec === "none") && f.audio_codec.startsWith("opus"),
  );
  if (preset.format === "opus") {
    const bytes = Math.max(0, ...audio.map(size));
    return bytes > 0 ? Math.round(bytes) : null;
  }
  // Container overhead and encoder variability make re-encoded video indeterminate.
  if (preset.format !== "mkv") return null;
  const video = formats.filter(
    (f) => f.height > 0 && (!preset.max_height || f.height <= preset.max_height),
  );
  const height = Math.max(0, ...video.map((f) => f.height));
  const candidates = video.filter((f) => f.height === height && size(f) > 0);
  const best = candidates.sort((a, b) => size(b) - size(a))[0];
  if (!best) return null;
  if (best.audio_codec && best.audio_codec !== "none") return Math.round(size(best));
  const audioBytes = Math.max(0, ...audio.map(size));
  return audioBytes > 0 ? Math.round(size(best) + audioBytes) : null;
}

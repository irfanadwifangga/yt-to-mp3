import { useState } from "react";
import { api, type Metadata, type Preset } from "./api";
import { Cover } from "./Cover";
import { t } from "./i18n";
import { formatDuration, messageFor, presetLabel } from "./messages";

interface Props {
  ready: boolean;
  presets: Preset[];
  defaultPreset: string;
  onQueued: () => void;
}

/** Kartu utama: tempel tautan, lihat pratinjau, lalu antrekan. */
export function Capture({ ready, presets, defaultPreset, onQueued }: Props) {
  const [url, setUrl] = useState("");
  const [presetId, setPresetId] = useState("");
  const [result, setResult] = useState<Metadata | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState<"analyze" | "queue" | null>(null);

  const selected =
    presetId ||
    presets.find((p) => p.id === defaultPreset)?.id ||
    presets[0]?.id ||
    "";

  async function analyze(target: string) {
    setBusy("analyze");
    setError(null);
    setResult(null);
    try {
      setResult(await api.metadata(target));
    } catch (err) {
      setError(messageFor(err, t("capture.failed")));
    } finally {
      setBusy(null);
    }
  }

  /** Menempel tautan adalah niat yang jelas; tidak perlu klik tambahan. */
  function handlePaste(e: React.ClipboardEvent<HTMLInputElement>) {
    const text = e.clipboardData.getData("text").trim();
    if (!/^https?:\/\/\S+$/i.test(text) || !ready || busy !== null) return;
    e.preventDefault();
    setUrl(text);
    void analyze(text);
  }

  async function handleConvert() {
    if (!selected || !result) return;
    setBusy("queue");
    setError(null);
    try {
      await api.createJob(url, selected);
      setResult(null);
      setUrl("");
      setPresetId("");
      onQueued();
    } catch (err) {
      setError(messageFor(err, t("capture.queueFailed")));
    } finally {
      setBusy(null);
    }
  }

  function clear() {
    setResult(null);
    setUrl("");
    setError(null);
  }

  return (
    <section className="capture" aria-labelledby="capture-label">
      <label id="capture-label" htmlFor="capture-url" className="capture-label">
        {t("capture.label")}
      </label>

      <form
        className="capture-row"
        onSubmit={(e) => {
          e.preventDefault();
          void analyze(url);
        }}
      >
        <input
          id="capture-url"
          type="url"
          inputMode="url"
          autoComplete="off"
          spellCheck={false}
          value={url}
          onChange={(e) => {
            setUrl(e.target.value);
            // Pratinjau milik tautan lama tidak boleh ikut terkirim.
            if (result) setResult(null);
          }}
          onPaste={handlePaste}
          placeholder="https://www.youtube.com/watch?v=…"
          aria-describedby="capture-hint"
          required
        />
        <button type="submit" className="btn primary" disabled={busy !== null || !ready}>
          {busy === "analyze" ? t("capture.busy") : t("capture.submit")}
        </button>
      </form>

      <p id="capture-hint" className="hint">
        {ready ? t("capture.pasteHint") : t("capture.needTool")}
      </p>

      {error && (
        <p className="alert" role="alert">
          {error}
        </p>
      )}

      {busy === "analyze" && <div className="preview is-loading" aria-hidden="true" />}

      {result && (
        <div className="preview">
          <Cover sourceKey={result.source_key} state="done" size="lg" />

          <div className="preview-body">
            <h2 className="preview-title">{result.title}</h2>
            <p className="preview-meta">
              <span>{result.uploader || t("capture.unknown")}</span>
              <span className="mono">{formatDuration(result.duration_ms)}</span>
              {result.source_codec && (
                <span className="mono">
                  {result.source_codec}
                  {result.sample_rate > 0 && ` · ${result.sample_rate / 1000} kHz`}
                </span>
              )}
            </p>

            <div className="preview-actions">
              <select
                value={selected}
                onChange={(e) => setPresetId(e.target.value)}
                aria-label={t("capture.preset")}
              >
                {presets.map((p) => (
                  <option key={p.id} value={p.id}>
                    {presetLabel(p)}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="btn primary"
                onClick={() => void handleConvert()}
                disabled={busy !== null || !selected}
              >
                {busy === "queue" ? t("capture.queueing") : t("capture.convert")}
              </button>
              <button type="button" className="btn ghost" onClick={clear} disabled={busy !== null}>
                {t("capture.clear")}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

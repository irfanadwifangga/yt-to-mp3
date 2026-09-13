import { useEffect, useRef, useState } from "react";
import { api, ApiError, MAX_TAG_LENGTH, type Job, type Metadata, type Preset } from "./api";
import { Cover } from "./Cover";
import { CloseIcon, FolderIcon } from "./icons";
import { t } from "./i18n";
import { formatDuration, formatTime, messageFor, presetLabel } from "./messages";

/**
 * Pola longgar yang memicu analisis otomatis.
 *
 * Validasi sebenarnya tetap di server (domain.NormalizeURL). Pola ini hanya
 * mencegah analisis terpicu oleh tautan yang belum selesai diketik, supaya
 * pengguna tidak melihat pesan error di tengah mengetik.
 */
const YOUTUBE_URL =
  /^https?:\/\/(www\.|m\.|music\.)?(youtube\.com\/(watch\?\S*v=|shorts\/|live\/)|youtu\.be\/)[\w-]{11}/i;

/** Jeda setelah berhenti mengetik sebelum analisis berjalan. */
const TYPE_DELAY_MS = 600;

interface Props {
  /**
   * Apakah yt-dlp tersedia. null berarti belum diketahui: health pertama
   * butuh beberapa detik karena versi tool diperiksa dengan menjalankan
   * binary-nya, dan selama itu input tidak boleh terkunci dengan pesan
   * seolah tool belum terpasang.
   */
  ready: boolean | null;
  presets: Preset[];
  defaultPreset: string;
  onQueued: (job: Job) => void;
}

/**
 * Kartu utama: tempel tautan, lihat pratinjau, lalu antrekan.
 *
 * Tidak ada tombol analisis. Tautan yang valid langsung dianalisis, jadi
 * tombol yang harus diklik hanya satu: Konversi.
 */
export function Capture({ ready, presets, defaultPreset, onQueued }: Props) {
  const [url, setUrl] = useState("");
  const [presetId, setPresetId] = useState("");
  const [result, setResult] = useState<Metadata | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Kode error disimpan terpisah dari teksnya supaya kegagalan karena
  // yt-dlp usang bisa menawarkan pembaruan, bukan sekadar "coba lagi".
  const [errorCode, setErrorCode] = useState<string | null>(null);
  const [busy, setBusy] = useState<"analyze" | "queue" | "update" | null>(null);
  // Judul dan artis yang akan ditulis ke tag dan nama berkas. Diisi saran
  // server setiap kali analisis selesai, lalu bebas disunting.
  const [tagTitle, setTagTitle] = useState("");
  const [tagArtist, setTagArtist] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  // Tautan yang sedang atau terakhir dianalisis. Hasil untuk tautan lain
  // dibuang, sehingga menempel tautan baru di tengah analisis tidak pernah
  // menampilkan pratinjau video yang salah.
  const analyzed = useRef("");

  const selected =
    presetId || presets.find((p) => p.id === defaultPreset)?.id || presets[0]?.id || "";

  async function analyze(target: string) {
    const link = target.trim();
    if (!link || ready !== true) return;

    analyzed.current = link;
    setBusy("analyze");
    setError(null);
    setErrorCode(null);
    setResult(null);
    try {
      const meta = await api.metadata(link);
      if (analyzed.current === link) {
        setResult(meta);
        setTagTitle(meta.suggested_title || meta.title);
        setTagArtist(meta.suggested_artist || meta.uploader);
      }
    } catch (err) {
      if (analyzed.current === link) {
        setError(messageFor(err, t("capture.failed")));
        setErrorCode(err instanceof ApiError ? err.code : null);
      }
    } finally {
      if (analyzed.current === link) setBusy(null);
    }
  }

  // Analisis otomatis setelah pengguna berhenti mengetik tautan yang valid.
  useEffect(() => {
    const link = url.trim();
    // Tautan yang ditempel saat status tool belum diketahui ikut dianalisis
    // begitu ready berubah menjadi true, karena ready ada di dependensi.
    if (ready !== true || busy === "queue" || !YOUTUBE_URL.test(link) || link === analyzed.current) {
      return;
    }
    const timer = setTimeout(() => void analyze(link), TYPE_DELAY_MS);
    return () => clearTimeout(timer);
    // analyze sengaja tidak masuk dependensi: fungsinya dibuat ulang setiap render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, ready]);

  /** Menempel tautan adalah niat yang jelas; analisis berjalan tanpa jeda. */
  function handlePaste(e: React.ClipboardEvent<HTMLInputElement>) {
    const text = e.clipboardData.getData("text").trim();
    if (!YOUTUBE_URL.test(text) || ready !== true || busy === "queue") return;
    e.preventDefault();
    setUrl(text);
    void analyze(text);
  }

  function handleChange(value: string) {
    setUrl(value);
    if (value.trim() !== analyzed.current) {
      // Pratinjau milik tautan lama tidak boleh ikut terkirim.
      analyzed.current = "";
      setResult(null);
      setError(null);
      setErrorCode(null);
      setBusy((current) => (current === "analyze" ? null : current));
    }
  }

  async function handleConvert() {
    if (!selected || !result) return;
    setBusy("queue");
    setError(null);
    try {
      const title = tagTitle.trim();
      const job = await api.createJob(url, selected, { title, artist: tagArtist.trim() });
      onQueued({ ...job, title: job.title || title || result.title });
      reset();
    } catch (err) {
      setError(messageFor(err, t("capture.queueFailed")));
    } finally {
      setBusy(null);
    }
  }

  function reset() {
    analyzed.current = "";
    setResult(null);
    setUrl("");
    setPresetId("");
    setError(null);
    setErrorCode(null);
    inputRef.current?.focus();
  }

  /** Memperbarui yt-dlp lalu mengulang analisis tautan yang sama. */
  async function updateAndAnalyze() {
    setBusy("update");
    setError(null);
    try {
      await api.updateTool("yt-dlp");
    } catch (err) {
      setError(messageFor(err, t("tools.updateFailed")));
      setBusy(null);
      return;
    }
    setBusy(null);
    await analyze(url);
  }

  async function revealPrevious(fileId: string) {
    try {
      await api.revealFile(fileId);
    } catch (err) {
      setError(messageFor(err, t("history.actionFailed")));
      setErrorCode(null);
    }
  }

  // Konversi sebelumnya dengan preset yang sedang dipilih. Preset lain
  // menghasilkan berkas berbeda, jadi bukan duplikat.
  const previous = result?.previous_conversions?.find((p) => p.preset_id === selected);

  // Saran atau suntingan yang berbeda dari metadata asli ditampilkan
  // berdampingan dengan aslinya, supaya tebakan yang keliru mudah dikenali.
  const edited =
    result !== null && (tagTitle.trim() !== result.title || tagArtist.trim() !== result.uploader);

  function restoreOriginal() {
    if (!result) return;
    setTagTitle(result.title);
    setTagArtist(result.uploader);
  }

  function convertOnEnter(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter" && busy === null) {
      e.preventDefault();
      void handleConvert();
    }
  }

  const status =
    ready === false
      ? t("capture.needTool")
      : busy === "update"
        ? t("tools.updating")
        : busy === "analyze"
        ? t("capture.busy")
        : ready === null
          ? t("capture.checkingTools")
          : t("capture.pasteHint");

  return (
    <section className="capture" aria-labelledby="capture-label">
      <label id="capture-label" htmlFor="capture-url" className="capture-label">
        {t("capture.label")}
      </label>

      {/* Enter tetap memicu analisis lewat submit implisit form satu input,
          termasuk untuk tautan yang tidak dikenali pola otomatis. */}
      <form
        className="capture-row"
        onSubmit={(e) => {
          e.preventDefault();
          void analyze(url);
        }}>
        <div className="capture-input">
          <input
            ref={inputRef}
            id="capture-url"
            type="url"
            inputMode="url"
            autoComplete="off"
            spellCheck={false}
            value={url}
            onChange={(e) => handleChange(e.target.value)}
            onPaste={handlePaste}
            placeholder="https://www.youtube.com/watch?v=…"
            aria-describedby="capture-hint"
            aria-busy={busy === "analyze"}
            disabled={ready === false}
          />
          {busy === "analyze" ? (
            <span className="capture-spinner" aria-hidden="true" />
          ) : (
            url && (
              <button
                type="button"
                className="icon-btn capture-clear"
                onClick={reset}
                aria-label={t("capture.clearInput")}>
                <CloseIcon />
              </button>
            )
          )}
        </div>
      </form>

      <p id="capture-hint" className="hint" role="status">
        {status}
      </p>

      {error && (
        <div className="alert capture-error" role="alert">
          <span>{error}</span>
          {errorCode === "TOOL_OUTDATED" ? (
            <button
              type="button"
              className="btn small primary"
              onClick={() => void updateAndAnalyze()}
              disabled={busy !== null}>
              {t("capture.updateYtdlp")}
            </button>
          ) : (
            <button
              type="button"
              className="btn small"
              onClick={() => void analyze(url)}
              disabled={busy !== null}>
              {t("capture.retry")}
            </button>
          )}
        </div>
      )}

      {busy === "analyze" && <div className="preview is-loading" aria-hidden="true" />}

      {result && (
        <div className="preview">
          <Cover sourceKey={result.source_key} state="done" size="lg" />

          <div className="preview-body">
            <div className="tag-fields">
              <label className="tag-field tag-field-title">
                <span className="tag-label">{t("capture.tagTitle")}</span>
                <input
                  type="text"
                  value={tagTitle}
                  maxLength={MAX_TAG_LENGTH}
                  onChange={(e) => setTagTitle(e.target.value)}
                  onKeyDown={convertOnEnter}
                  placeholder={result.title}
                  aria-describedby="tag-hint"
                />
              </label>
              <label className="tag-field">
                <span className="tag-label">{t("capture.tagArtist")}</span>
                <input
                  type="text"
                  value={tagArtist}
                  maxLength={MAX_TAG_LENGTH}
                  onChange={(e) => setTagArtist(e.target.value)}
                  onKeyDown={convertOnEnter}
                  placeholder={result.uploader || t("capture.unknown")}
                  aria-describedby="tag-hint"
                />
              </label>
            </div>
            <p id="tag-hint" className="tag-hint">
              {edited ? (
                <>
                  <span className="tag-original" title={result.title}>
                    {t("capture.tagOriginal", { title: result.title })}
                  </span>
                  <button type="button" className="link-btn" onClick={restoreOriginal}>
                    {t("capture.tagRestore")}
                  </button>
                </>
              ) : (
                t("capture.tagHint")
              )}
            </p>
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

            {previous && (
              <div className="capture-previous" role="note">
                <span>
                  {t("capture.previous", { time: formatTime(previous.finished_at ?? "") })}
                </span>
                <button
                  type="button"
                  className="btn small"
                  onClick={() => void revealPrevious(previous.file_id)}>
                  <FolderIcon />
                  {t("history.reveal")}
                </button>
              </div>
            )}

            <div className="preview-actions">
              <select
                value={selected}
                onChange={(e) => setPresetId(e.target.value)}
                aria-label={t("capture.preset")}>
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
                disabled={busy !== null || !selected}>
                {busy === "queue"
                  ? t("capture.queueing")
                  : previous
                    ? t("capture.convertAgain")
                    : t("capture.convert")}
              </button>
              <button type="button" className="btn ghost" onClick={reset} disabled={busy !== null}>
                {t("capture.clear")}
              </button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

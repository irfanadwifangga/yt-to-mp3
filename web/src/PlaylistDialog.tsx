import { useEffect, useRef, useState } from "react";
import { api, ApiError, type Job, type Playlist, type Preset } from "./api";
import { Modal } from "./Modal";
import { Select } from "./Select";
import { t } from "./i18n";
import { formatDuration, messageFor, messageForCode, presetLabel } from "./messages";

type Outcome = { status: "queued" | "skipped" | "failed"; message?: string };
export function PlaylistDialog({
  url,
  presets,
  defaultPreset,
  onQueued,
  onClose,
}: {
  url: string;
  presets: Preset[];
  defaultPreset: string;
  onQueued: (job: Job) => void;
  onClose: () => void;
}) {
  const [list, setList] = useState<Playlist | null>(null);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [outcomes, setOutcomes] = useState<Record<number, Outcome>>({});
  const [presetID, setPresetID] = useState(defaultPreset);
  const [search, setSearch] = useState("");
  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false),
    stop = useRef(false),
    disposed = useRef(false);
  useEffect(() => {
    const controller = new AbortController();
    disposed.current = false;
    setError("");
    setList(null);
    void api
      .playlist(url, controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        setList(result);
        setSelected(new Set(result.entries.flatMap((e, i) => (e.unavailable ? [] : [i]))));
        setOutcomes({});
      })
      .catch((err) => {
        if (!controller.signal.aborted) setError(messageFor(err));
      });
    return () => {
      controller.abort();
      stop.current = true;
      disposed.current = true;
    };
  }, [url, revision]);
  const preset =
    presets.find((p) => p.id === presetID) ??
    presets.find((p) => p.id === defaultPreset) ??
    presets[0];
  const formats = [...new Set(presets.filter((p) => p.kind === preset?.kind).map((p) => p.format))];
  const choices = presets.filter((p) => p.kind === preset?.kind && p.format === preset?.format);
  const selectable =
    list?.entries.flatMap((e, i) =>
      !e.unavailable && outcomes[i]?.status !== "queued" && outcomes[i]?.status !== "skipped"
        ? [i]
        : [],
    ) ?? [];
  const pending = selectable.filter((i) => selected.has(i));
  const all = selectable.length > 0 && selectable.every((i) => selected.has(i));
  const counts = Object.values(outcomes).reduce(
    (sum, o) => ({ ...sum, [o.status]: sum[o.status] + 1 }),
    { queued: 0, failed: 0, skipped: 0 },
  );
  async function enqueue() {
    if (busyRef.current || !list || !preset || pending.length === 0) return;
    busyRef.current = true;
    stop.current = false;
    setBusy(true);
    setError("");
    const seen = new Set(
      list.entries.filter((_, i) => outcomes[i]?.status === "queued").map((e) => e.url),
    );
    function record(index: number, result: Outcome) {
      if (!disposed.current) setOutcomes((previous) => ({ ...previous, [index]: result }));
    }
    try {
      for (const index of pending) {
        if (stop.current) break;
        const entry = list.entries[index]!;
        if (seen.has(entry.url)) {
          record(index, { status: "skipped", message: messageForCode("DUPLICATE_ACTIVE") });
          continue;
        }
        seen.add(entry.url);
        try {
          const job = await api.createJob(entry.url, preset.id);
          record(index, { status: "queued" });
          onQueued({ ...job, title: job.title || entry.title });
        } catch (err) {
          const code = err instanceof ApiError ? err.code : "NETWORK";
          record(index, {
            status: code === "DUPLICATE_ACTIVE" ? "skipped" : "failed",
            message: messageFor(err),
          });
          if (["QUEUE_FULL", "UNAUTHORIZED", "TIMEOUT", "RATE_LIMITED", "NETWORK"].includes(code))
            break;
        }
      }
    } finally {
      busyRef.current = false;
      if (!disposed.current) setBusy(false);
    }
  }
  return (
    <Modal
      title={t("playlist.title")}
      dismissible={!busy}
      onClose={() => {
        if (!busyRef.current) onClose();
      }}>
      <div className="playlist-panel">
        {error && (
          <p className="alert" role="alert">
            {error}{" "}
            <button
              type="button"
              className="link"
              onClick={() => setRevision((n) => n + 1)}
              disabled={busy}>
              {t("capture.retry")}
            </button>
          </p>
        )}
        {!list && !error && <p role="status">{t("playlist.loading")}</p>}
        {list && (
          <>
            <h3>{list.title || t("playlist.title")}</h3>
            <p className="hint">{t("playlist.hint")}</p>
            {list.truncated && <p className="hint warn">{t("playlist.limited")}</p>}
            <div className="playlist-output">
              <label>
                {t("capture.kind")}
                <Select
                  value={preset?.kind ?? "audio"}
                  onChange={(e) =>
                    setPresetID(presets.find((p) => p.kind === e.target.value)?.id ?? "")
                  }
                  disabled={busy}>
                  {["audio", "video"]
                    .filter((k) => presets.some((p) => p.kind === k))
                    .map((k) => (
                      <option key={k} value={k}>
                        {t(k === "audio" ? "capture.kindAudio" : "capture.kindVideo")}
                      </option>
                    ))}
                </Select>
              </label>
              <label>
                {t("capture.format")}
                <Select
                  value={preset?.format ?? ""}
                  onChange={(e) =>
                    setPresetID(
                      presets.find((p) => p.kind === preset?.kind && p.format === e.target.value)
                        ?.id ?? "",
                    )
                  }
                  disabled={busy}>
                  {formats.map((f) => (
                    <option key={f} value={f}>
                      {f.toUpperCase()}
                    </option>
                  ))}
                </Select>
              </label>
              <label>
                {t("capture.preset")}
                <Select
                  value={preset?.id ?? ""}
                  onChange={(e) => setPresetID(e.target.value)}
                  disabled={busy}>
                  {choices.map((p) => (
                    <option key={p.id} value={p.id}>
                      {presetLabel(p)}
                    </option>
                  ))}
                </Select>
              </label>
            </div>
            <div className="playlist-toolbar">
              <label>
                <input
                  type="checkbox"
                  checked={all}
                  disabled={busy || selectable.length === 0}
                  onChange={() => setSelected(all ? new Set() : new Set(selectable))}
                />{" "}
                {t("playlist.selectAll")}
              </label>
              <input
                type="search"
                aria-label={t("playlist.search")}
                placeholder={t("playlist.search")}
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </div>
            <div className="playlist-entries">
              {list.entries
                .map((entry, index) => ({ entry, index }))
                .filter(({ entry }) =>
                  entry.title.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
                )
                .map(({ entry, index }) => (
                  <div className="playlist-entry" key={index}>
                    <label>
                      <input
                        type="checkbox"
                        aria-label={entry.title || t("playlist.unavailable")}
                        checked={selected.has(index)}
                        disabled={
                          busy ||
                          entry.unavailable ||
                          outcomes[index]?.status === "queued" ||
                          outcomes[index]?.status === "skipped"
                        }
                        onChange={() =>
                          setSelected((previous) => {
                            const next = new Set(previous);
                            if (next.has(index)) next.delete(index);
                            else next.add(index);
                            return next;
                          })
                        }
                      />
                      <span>
                        <strong>{entry.title || t("playlist.unavailable")}</strong>
                        <small>
                          {outcomes[index]?.message ||
                            (entry.unavailable
                              ? t("playlist.unavailable")
                              : outcomes[index]
                                ? t(`playlist.${outcomes[index]!.status}`)
                                : entry.duration_ms > 0
                                  ? formatDuration(entry.duration_ms)
                                  : "—")}
                        </small>
                      </span>
                    </label>
                  </div>
                ))}
              {list.entries.length === 0 && <p className="empty">{t("playlist.empty")}</p>}
            </div>
            <footer className="playlist-footer">
              <p role="status">{t("playlist.summary", counts)}</p>
              {busy ? (
                <button
                  type="button"
                  className="btn"
                  onClick={() => {
                    stop.current = true;
                  }}>
                  {t("playlist.stop")}
                </button>
              ) : (
                <button
                  type="button"
                  className="btn primary"
                  disabled={!preset || pending.length === 0}
                  onClick={() => void enqueue()}>
                  {t("playlist.add", { count: pending.length })}
                </button>
              )}
            </footer>
          </>
        )}
      </div>
    </Modal>
  );
}

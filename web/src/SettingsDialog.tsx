import { useEffect, useRef, useState } from "react";
import { api, ApiError, type Health, type Preset, type Setting } from "./api";
import { CloseIcon, FolderIcon } from "./icons";
import { has, locale, setLocale, t, type Locale } from "./i18n";
import { formatTime, messageFor, presetLabel } from "./messages";
import { loadTheme, saveTheme, type Theme } from "./theme";

export type SettingsSection =
  | "storage"
  | "conversion"
  | "queue"
  | "tools"
  | "display"
  | "advanced"
  | "about";

/**
 * Pengelompokan setelan. Kunci yang belum dikenal UI jatuh ke "Lanjutan",
 * sehingga setelan baru dari server tetap bisa diubah tanpa menunggu UI
 * diperbarui.
 */
const GROUPS: { id: SettingsSection; keys: string[] }[] = [
  { id: "storage", keys: ["output_dir"] },
  { id: "conversion", keys: ["default_preset_id", "filename_mode"] },
  { id: "queue", keys: ["max_concurrent_jobs", "max_queue_depth"] },
  { id: "tools", keys: ["tool_update_check"] },
  { id: "display", keys: [] },
  { id: "advanced", keys: ["idle_shutdown_minutes", "log_level"] },
  { id: "about", keys: [] },
];

const KNOWN_KEYS = new Set(GROUPS.flatMap((g) => g.keys));

/** Batas yang sama dengan validasi server, supaya kontrol angka tidak
 *  menawarkan nilai yang pasti ditolak. */
const RANGES: Record<string, { min: number; max: number }> = {
  max_concurrent_jobs: { min: 1, max: 8 },
  max_queue_depth: { min: 1, max: 500 },
  idle_shutdown_minutes: { min: 0, max: 1440 },
};

interface Props {
  /** Bagian yang dituju saat dibuka; null berarti dialog tertutup. */
  section: SettingsSection | null;
  health: Health | null;
  presets: Preset[];
  onClose: () => void;
  onChanged: () => void;
}

export function SettingsDialog({ section, health, presets, onClose, onChanged }: Props) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const open = section !== null;

  const [settings, setSettings] = useState<Setting[] | null>(null);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [badKey, setBadKey] = useState<string | null>(null);
  const [notice, setNotice] = useState<"saved" | "restart" | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;

    if (open && !dialog.open) {
      dialog.showModal();
      // Setiap kali dibuka, mulai dari nilai server yang terbaru.
      setDraft({});
      setError(null);
      setBadKey(null);
      setNotice(null);
      api
        .settings()
        .then((res) => setSettings(res.settings))
        .catch((err) => setError(messageFor(err, t("settings.loadFailed"))));
    } else if (!open && dialog.open) {
      dialog.close();
    }
  }, [open]);

  // Lompat ke bagian yang diminta setelah isinya tersedia.
  useEffect(() => {
    if (!open || !section || settings === null) return;
    scrollToGroup(section, "instant");
  }, [open, section, settings]);

  function scrollToGroup(id: SettingsSection, behavior: ScrollBehavior = "smooth") {
    const target = bodyRef.current?.querySelector<HTMLElement>(`#settings-${id}`);
    target?.scrollIntoView({ block: "start", behavior });
  }

  function change(key: string, value: string) {
    setDraft((prev) => ({ ...prev, [key]: value }));
    setNotice(null);
    if (badKey === key) setBadKey(null);
  }

  const original = new Map(settings?.map((s) => [s.key, s.value]) ?? []);
  const changed = Object.keys(draft).filter((k) => draft[k] !== original.get(k));

  async function save() {
    if (changed.length === 0) return;
    setBusy(true);
    setError(null);
    try {
      const values: Record<string, string> = {};
      for (const k of changed) values[k] = draft[k] ?? "";
      const res = await api.updateSettings(values);
      const restart = res.settings.some((s) => s.requires_restart && changed.includes(s.key));
      setSettings(res.settings);
      setDraft({});
      setBadKey(null);
      setNotice(restart ? "restart" : "saved");
      onChanged();
    } catch (err) {
      setError(messageFor(err, t("settings.saveFailed")));
      // Server menyebutkan kunci yang ditolak, sehingga kesalahan bisa
      // ditunjukkan tepat di fieldnya.
      const key = err instanceof ApiError ? (err.details.key ?? null) : null;
      setBadKey(key);
      if (key) {
        const group = GROUPS.find((g) => g.keys.includes(key))?.id ?? "advanced";
        scrollToGroup(group);
      }
    } finally {
      setBusy(false);
    }
  }

  function fieldsFor(groupId: SettingsSection, keys: string[]) {
    if (!settings) return [];
    if (groupId === "advanced") {
      return settings.filter((s) => keys.includes(s.key) || !KNOWN_KEYS.has(s.key));
    }
    return keys.flatMap((k) => settings.filter((s) => s.key === k));
  }

  return (
    <dialog
      ref={dialogRef}
      className="settings"
      aria-labelledby="settings-title"
      onClose={onClose}
      onClick={(e) => {
        // Klik pada backdrop menutup dialog; klik di dalam lembar tidak.
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="sheet">
        <header className="sheet-head">
          <h2 id="settings-title">{t("settings.title")}</h2>
          <button
            type="button"
            className="icon-btn"
            onClick={onClose}
            aria-label={t("settings.close")}
          >
            <CloseIcon />
          </button>
        </header>

        <nav className="sheet-nav" aria-label={t("settings.title")}>
          {GROUPS.map((g) => (
            <button key={g.id} type="button" onClick={() => scrollToGroup(g.id)}>
              {t(`settings.group.${g.id}`)}
            </button>
          ))}
        </nav>

        <div className="sheet-body" ref={bodyRef}>
          {error && (
            <p className="alert" role="alert">
              {error}
            </p>
          )}

          {GROUPS.map((g) => (
            <section key={g.id} id={`settings-${g.id}`} className="group">
              <h3 className="group-title">{t(`settings.group.${g.id}`)}</h3>

              {fieldsFor(g.id, g.keys).map((s) => (
                <Field
                  key={s.key}
                  setting={s}
                  value={draft[s.key] ?? s.value}
                  dirty={changed.includes(s.key)}
                  invalid={badKey === s.key}
                  presets={presets}
                  onChange={(v) => change(s.key, v)}
                />
              ))}

              {g.id === "tools" && <ToolsPanel health={health} onChanged={onChanged} />}
              {g.id === "display" && <DisplayPanel />}
              {g.id === "about" && <AboutPanel health={health} />}
            </section>
          ))}
        </div>

        <footer className="sheet-foot">
          <p className="sheet-status" role="status">
            {changed.length > 0
              ? t("settings.unsaved", { count: changed.length })
              : notice === "restart"
                ? t("settings.restartNotice")
                : notice === "saved"
                  ? t("settings.saved")
                  : ""}
          </p>
          {changed.length > 0 && (
            <button type="button" className="btn ghost" onClick={() => setDraft({})} disabled={busy}>
              {t("settings.discard")}
            </button>
          )}
          <button
            type="button"
            className="btn primary"
            onClick={() => void save()}
            disabled={busy || changed.length === 0}
          >
            {busy ? t("settings.saving") : t("settings.save")}
          </button>
        </footer>
      </div>
    </dialog>
  );
}

/* Field setelan -------------------------------------------------------- */

function labelFor(key: string): string {
  const k = `settings.label.${key}`;
  return has(k) ? t(k) : key;
}

function hintFor(key: string): string | undefined {
  const k = `settings.hint.${key}`;
  return has(k) ? t(k) : undefined;
}

/** Pilihan teknis seperti tingkat log ditampilkan apa adanya; hanya yang
 *  punya padanan ramah yang diterjemahkan. */
function optionLabel(settingKey: string, option: string, presets: Preset[]): string {
  if (settingKey === "default_preset_id") {
    const preset = presets.find((p) => p.id === option);
    if (preset) return presetLabel(preset);
  }
  const k = `settings.option.${settingKey}.${option}`;
  return has(k) ? t(k) : option;
}

interface FieldProps {
  setting: Setting;
  value: string;
  dirty: boolean;
  invalid: boolean;
  presets: Preset[];
  onChange: (value: string) => void;
}

function Field({ setting, value, dirty, invalid, presets, onChange }: FieldProps) {
  const id = `setting-${setting.key}`;
  const hint = hintFor(setting.key);
  const hintId = hint ? `${id}-hint` : undefined;

  let control: React.ReactNode;
  switch (setting.kind) {
    case "path":
      control = <PathControl id={id} value={value} describedBy={hintId} onChange={onChange} />;
      break;
    case "enum":
      control = (
        <select id={id} value={value} onChange={(e) => onChange(e.target.value)} aria-describedby={hintId}>
          {setting.options?.map((opt) => (
            <option key={opt} value={opt}>
              {optionLabel(setting.key, opt, presets)}
            </option>
          ))}
        </select>
      );
      break;
    case "int":
      control = (
        <input
          id={id}
          type="number"
          className="mono narrow"
          inputMode="numeric"
          min={RANGES[setting.key]?.min}
          max={RANGES[setting.key]?.max}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-describedby={hintId}
        />
      );
      break;
    case "bool":
      control = (
        <label className="switch">
          <input
            id={id}
            type="checkbox"
            checked={value === "true"}
            onChange={(e) => onChange(String(e.target.checked))}
          />
          <span>{value === "true" ? t("settings.on") : t("settings.off")}</span>
        </label>
      );
      break;
    default:
      control = (
        <input id={id} type="text" value={value} onChange={(e) => onChange(e.target.value)} aria-describedby={hintId} />
      );
  }

  const className = ["field", dirty ? "is-dirty" : "", invalid ? "is-invalid" : ""]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={className}>
      <div className="field-label">
        <label htmlFor={id}>{labelFor(setting.key)}</label>
        {setting.requires_restart && <span className="badge">{t("settings.restartBadge")}</span>}
      </div>
      {control}
      {hint && (
        <p id={hintId} className="hint">
          {hint}
        </p>
      )}
    </div>
  );
}

interface PathProps {
  id: string;
  value: string;
  describedBy?: string;
  onChange: (value: string) => void;
}

/**
 * Folder dipilih lewat dialog native. Input teks tetap bisa diketik sebagai
 * jalan keluar bila pemilih tidak tersedia, misalnya Linux tanpa zenity.
 */
function PathControl({ id, value, describedBy, onChange }: PathProps) {
  const [picking, setPicking] = useState(false);
  const [unavailable, setUnavailable] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function pick() {
    setPicking(true);
    setError(null);
    try {
      const res = await api.pickFolder(t("settings.pickFolderTitle"), value);
      if (!res.cancelled && res.path) onChange(res.path);
    } catch (err) {
      if (err instanceof ApiError && err.code === "DIALOG_UNAVAILABLE") setUnavailable(true);
      else setError(messageFor(err));
    } finally {
      setPicking(false);
    }
  }

  return (
    <>
      <div className="path-control">
        <span className="path-icon">
          <FolderIcon />
        </span>
        <input
          id={id}
          type="text"
          className="mono"
          spellCheck={false}
          autoComplete="off"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-describedby={describedBy}
        />
        <button type="button" className="btn" onClick={() => void pick()} disabled={picking}>
          {picking ? t("settings.picking") : t("settings.pickFolder")}
        </button>
      </div>
      {unavailable && <p className="hint warn">{t("settings.pickerUnavailable")}</p>}
      {error && (
        <p className="alert small" role="alert">
          {error}
        </p>
      )}
    </>
  );
}

/* Panel non-setelan ---------------------------------------------------- */

function ToolsPanel({ health, onChanged }: { health: Health | null; onChanged: () => void }) {
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [checkedAt, setCheckedAt] = useState<string | undefined>();

  useEffect(() => {
    api
      .tools()
      .then((res) => setCheckedAt(res.checked_at))
      .catch(() => setCheckedAt(undefined));
  }, []);

  async function run(key: string, failure: string, fn: () => Promise<{ checked_at?: string }>) {
    setBusy(key);
    setError(null);
    try {
      const res = await fn();
      setCheckedAt(res.checked_at);
      onChanged();
    } catch (err) {
      setError(messageFor(err, failure));
    } finally {
      setBusy(null);
    }
  }

  if (!health) return null;

  return (
    <>
      <p className="hint">{t("tools.hint")}</p>
      {error && (
        <p className="alert small" role="alert">
          {error}
        </p>
      )}
      <ul className="tools">
        {Object.entries(health.tools).map(([name, tool]) => (
          <li key={name}>
            <span className={tool.available ? (tool.update_available ? "dot warn" : "dot ok") : "dot warn"} />
            <span className="tool-name">{name}</span>
            <span className="mono tool-version">
              {tool.available ? tool.version || t("tools.installed") : t("tools.unavailable")}
              {tool.update_available && tool.latest_version && (
                <span className="tool-latest"> → {t("tools.newVersion", { version: tool.latest_version })}</span>
              )}
            </span>
            <ToolAction
              name={name}
              tool={tool}
              busy={busy}
              onInstall={() => void run(name, t("tools.installFailed"), () => api.installTool(name))}
              onUpdate={() => void run(name, t("tools.updateFailed"), () => api.updateTool(name))}
            />
          </li>
        ))}
      </ul>
      <div className="tools-check">
        <span className="hint">
          {checkedAt
            ? t("tools.lastChecked", { time: formatTime(checkedAt) })
            : t("tools.neverChecked")}
        </span>
        <button
          type="button"
          className="btn small"
          disabled={busy !== null}
          onClick={() => void run("check", t("tools.checkFailed"), () => api.checkToolUpdates())}>
          {busy === "check" ? t("tools.checking") : t("tools.checkNow")}
        </button>
      </div>
    </>
  );
}

interface ToolActionProps {
  name: string;
  tool: Health["tools"][string];
  busy: string | null;
  onInstall: () => void;
  onUpdate: () => void;
}

/** Tombol atau petunjuk di ujung baris tool. */
function ToolAction({ name, tool, busy, onInstall, onUpdate }: ToolActionProps) {
  // ffprobe ikut terpasang dan diperbarui bersama ffmpeg dari arsip yang sama.
  if (name === "ffprobe") {
    return !tool.available ? <span className="hint">{t("tools.bundled")}</span> : null;
  }

  if (!tool.available) {
    return (
      <button type="button" className="btn small" onClick={onInstall} disabled={busy !== null}>
        {busy === name ? t("tools.installing") : t("tools.install")}
      </button>
    );
  }

  if (!tool.update_available) return null;

  // Hanya yt-dlp yang dapat diperbarui dari aplikasi. FFmpeg mengikuti
  // manifest ter-pin di rilis aplikasi, atau package manager bila berasal
  // dari PATH.
  if (name === "yt-dlp") {
    return (
      <button type="button" className="btn small primary" onClick={onUpdate} disabled={busy !== null}>
        {busy === name ? t("tools.updating") : t("tools.update")}
      </button>
    );
  }
  return (
    <span className="hint">
      {tool.source === "path" ? t("tools.updateViaPackageManager") : t("tools.updateViaRelease")}
    </span>
  );
}

function DisplayPanel() {
  const [theme, setTheme] = useState<Theme>(loadTheme);

  function chooseTheme(next: Theme) {
    setTheme(next);
    saveTheme(next);
  }

  return (
    <>
      <div className="field">
        <div className="field-label">
          <label htmlFor="setting-language">{t("settings.label.language")}</label>
        </div>
        {/* Nama bahasa sengaja ditulis dalam bahasanya sendiri, supaya tetap
            terbaca oleh orang yang tidak memahami bahasa yang sedang aktif. */}
        <select
          id="setting-language"
          value={locale}
          onChange={(e) => setLocale(e.target.value as Locale)}
          aria-describedby="setting-language-hint"
        >
          <option value="id">Bahasa Indonesia</option>
          <option value="en">English</option>
        </select>
        <p id="setting-language-hint" className="hint">
          {t("settings.hint.language")}
        </p>
      </div>

      <fieldset className="field">
        <legend className="field-label">{t("settings.label.theme")}</legend>
        <div className="segmented">
          {(["system", "light", "dark"] as const).map((option) => (
            <label key={option}>
              <input
                type="radio"
                name="theme"
                value={option}
                checked={theme === option}
                onChange={() => chooseTheme(option)}
              />
              <span>{t(`settings.theme.${option}`)}</span>
            </label>
          ))}
        </div>
      </fieldset>
    </>
  );
}

function AboutPanel({ health }: { health: Health | null }) {
  if (!health) return null;
  return (
    <dl className="about">
      <dt>{t("settings.about.version")}</dt>
      <dd className="mono">{health.version}</dd>
      <dt>{t("settings.about.commit")}</dt>
      <dd className="mono">{health.commit}</dd>
    </dl>
  );
}

import { useEffect, useState } from "react";
import { api, ApiError, type Setting } from "./api";
import { has, t } from "./i18n";
import { messageFor } from "./messages";

/** Label yang lebih ramah daripada nama kuncinya. */
function labelFor(key: string): string {
  const k = `settings.label.${key}`;
  return has(k) ? t(k) : key;
}

/** Penjelasan hanya untuk setelan yang pilihannya tidak jelas dengan
 *  sendirinya. */
function hintFor(key: string): string | undefined {
  const k = `settings.hint.${key}`;
  return has(k) ? t(k) : undefined;
}

/** Pilihan teknis seperti id preset dan tingkat log ditampilkan apa adanya;
 *  hanya yang punya padanan ramah yang diterjemahkan. */
function optionLabel(settingKey: string, option: string): string {
  const k = `settings.option.${settingKey}.${option}`;
  return has(k) ? t(k) : option;
}

export function Settings() {
  const [settings, setSettings] = useState<Setting[]>([]);
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);
  const [badKey, setBadKey] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .settings()
      .then((res) => setSettings(res.settings))
      .catch((err) => setError(messageFor(err, t("settings.loadFailed"))));
  }, []);

  function change(key: string, value: string) {
    setDraft((prev) => ({ ...prev, [key]: value }));
    setSaved(false);
  }

  async function save() {
    if (Object.keys(draft).length === 0) return;
    setBusy(true);
    setError(null);
    try {
      const res = await api.updateSettings(draft);
      setSettings(res.settings);
      setDraft({});
      setSaved(true);
      setBadKey(null);
    } catch (err) {
      setError(messageFor(err, t("settings.saveFailed")));
      // Server menyebutkan kunci mana yang ditolak, sehingga kesalahan bisa
      // ditunjukkan tepat di fieldnya alih-alih sebagai pesan umum.
      setBadKey(err instanceof ApiError ? (err.details.key ?? null) : null);
    } finally {
      setBusy(false);
    }
  }

  if (settings.length === 0) return null;

  const changed = Object.keys(draft);
  // Restart hanya diberitahukan bila setelan yang bersangkutan memang diubah.
  const needsRestart = settings.some((s) => changed.includes(s.key) && s.requires_restart);

  return (
    <section>
      <h2>{t("settings.title")}</h2>
      {error && <p className="error">{error}</p>}

      <dl className="grid">
        {settings.map((s) => (
          <SettingRow
            key={s.key}
            setting={s}
            value={draft[s.key] ?? s.value}
            invalid={badKey === s.key}
            onChange={(v) => change(s.key, v)}
          />
        ))}
      </dl>

      <div className="actions">
        <button type="button" onClick={() => void save()} disabled={busy || changed.length === 0}>
          {busy ? t("settings.saving") : t("settings.save")}
        </button>
        {changed.length > 0 && (
          <button type="button" onClick={() => setDraft({})} disabled={busy}>
            {t("settings.discard")}
          </button>
        )}
        {saved && <span className="muted small">{t("settings.saved")}</span>}
        {needsRestart && <span className="muted small">{t("settings.restartNotice")}</span>}
      </div>
    </section>
  );
}

interface RowProps {
  setting: Setting;
  value: string;
  invalid: boolean;
  onChange: (value: string) => void;
}

function SettingRow({ setting, value, invalid, onChange }: RowProps) {
  const label = labelFor(setting.key);
  const hint = hintFor(setting.key);

  return (
    <>
      <dt>
        {label}
        {setting.requires_restart && (
          <span className="muted small"> · {t("settings.restartBadge")}</span>
        )}
      </dt>
      <dd className={invalid ? "invalid" : undefined}>
        {setting.kind === "enum" ? (
          <select value={value} onChange={(e) => onChange(e.target.value)} aria-label={label}>
            {setting.options?.map((opt) => (
              <option key={opt} value={opt}>
                {optionLabel(setting.key, opt)}
              </option>
            ))}
          </select>
        ) : setting.kind === "bool" ? (
          <label className="check">
            <input
              type="checkbox"
              checked={value === "true"}
              onChange={(e) => onChange(String(e.target.checked))}
            />
            <span>{value === "true" ? t("settings.on") : t("settings.off")}</span>
          </label>
        ) : (
          <input
            type={setting.kind === "int" ? "number" : "text"}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            aria-label={label}
          />
        )}
        {hint && <div className="muted small">{hint}</div>}
      </dd>
    </>
  );
}

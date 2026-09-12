import { useEffect, useState } from "react";
import { api, ApiError, type Setting } from "./api";
import { messageFor } from "./messages";

/** Label yang lebih ramah daripada nama kuncinya. */
const LABELS: Record<string, string> = {
  output_dir: "Direktori keluaran",
  default_preset_id: "Preset bawaan",
  filename_mode: "Pola nama berkas",
  max_concurrent_jobs: "Job paralel",
  max_queue_depth: "Kapasitas antrean",
  idle_shutdown_minutes: "Berhenti otomatis (menit)",
  tool_update_check: "Cek pembaruan tool",
  log_level: "Tingkat log",
};

/** Penjelasan hanya untuk setelan yang pilihannya tidak jelas dengan
 *  sendirinya. */
const HINTS: Record<string, string> = {
  max_concurrent_jobs:
    "Lebih dari 2–3 unduhan paralel dari satu IP memicu pembatasan dari sisi sumber.",
  idle_shutdown_minutes: "Nol berarti aplikasi tidak berhenti sendiri.",
};

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
      .catch((err) => setError(messageFor(err, "Gagal memuat setelan")));
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
      setError(messageFor(err, "Penyimpanan gagal"));
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
  const needsRestart = settings.some(
    (s) => changed.includes(s.key) && s.requires_restart,
  );

  return (
    <section>
      <h2>Setelan</h2>
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
          {busy ? "Menyimpan..." : "Simpan"}
        </button>
        {changed.length > 0 && (
          <button type="button" onClick={() => setDraft({})} disabled={busy}>
            Batalkan perubahan
          </button>
        )}
        {saved && <span className="muted small">Tersimpan.</span>}
        {needsRestart && (
          <span className="muted small">Sebagian perubahan berlaku setelah aplikasi dibuka ulang.</span>
        )}
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
  const label = LABELS[setting.key] ?? setting.key;
  const hint = HINTS[setting.key];

  return (
    <>
      <dt>
        {label}
        {setting.requires_restart && <span className="muted small"> · perlu restart</span>}
      </dt>
      <dd className={invalid ? "invalid" : undefined}>
        {setting.kind === "enum" ? (
          <select value={value} onChange={(e) => onChange(e.target.value)} aria-label={label}>
            {setting.options?.map((opt) => (
              <option key={opt} value={opt}>
                {opt}
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
            <span>{value === "true" ? "aktif" : "nonaktif"}</span>
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

import { useEffect, useState } from "react";
import { CloseIcon, FolderIcon } from "./icons";
import { t } from "./i18n";
import { messageFor } from "./messages";

/** Satu tombol tindakan pada notifikasi. */
export interface ToastAction {
  label: string;
  icon?: "folder";
  run: () => Promise<unknown>;
}

export interface Toast {
  id: string;
  tone: "ok" | "bad" | "info";
  title: string;
  body?: string;
  action?: ToastAction;
}

/** Konfirmasi singkat cukup sekejap; hasil konversi perlu waktu untuk dibaca
 *  dan ditindaklanjuti. */
const DURATION_MS: Record<Toast["tone"], number> = { ok: 12000, bad: 12000, info: 4000 };

interface Props {
  toasts: Toast[];
  onDismiss: (id: string) => void;
}

export function Toasts({ toasts, onDismiss }: Props) {
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} onDismiss={onDismiss} />
      ))}
    </div>
  );
}

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: (id: string) => void }) {
  // Notifikasi yang sedang disorot, difokus, atau menjalankan tindakan tidak
  // hilang di tengah dibaca.
  const [held, setHeld] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (held || busy || error) return;
    const timer = setTimeout(() => onDismiss(toast.id), DURATION_MS[toast.tone]);
    return () => clearTimeout(timer);
  }, [held, busy, error, toast.id, toast.tone, onDismiss]);

  async function run() {
    if (!toast.action) return;
    setBusy(true);
    setError(null);
    try {
      await toast.action.run();
      onDismiss(toast.id);
    } catch (err) {
      setError(messageFor(err, t("history.actionFailed")));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className={`toast toast-${toast.tone}`}
      role={toast.tone === "bad" ? "alert" : "status"}
      onMouseEnter={() => setHeld(true)}
      onMouseLeave={() => setHeld(false)}
      onFocus={() => setHeld(true)}
      onBlur={() => setHeld(false)}>
      <span className="toast-mark" aria-hidden="true" />
      <div className="toast-body">
        <p className="toast-title" title={toast.title}>
          {toast.title}
        </p>
        {toast.body && <p className="toast-text">{toast.body}</p>}
        {error && <p className="toast-text toast-error">{error}</p>}
      </div>
      <div className="toast-actions">
        {toast.action && (
          <button
            type="button"
            className="btn small primary"
            onClick={() => void run()}
            disabled={busy}>
            {toast.action.icon === "folder" && <FolderIcon />}
            {busy ? t("toast.working") : toast.action.label}
          </button>
        )}
        <button
          type="button"
          className="icon-btn"
          onClick={() => onDismiss(toast.id)}
          aria-label={t("toast.close")}>
          <CloseIcon />
        </button>
      </div>
    </div>
  );
}

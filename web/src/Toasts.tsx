import { useEffect, useState } from "react";
import { CloseIcon, FolderIcon } from "./icons";
import { t } from "./i18n";
import { messageFor } from "./messages";

export interface Toast {
  id: string;
  tone: "ok" | "bad" | "info";
  title: string;
  body?: string;
  /** Terisi bila notifikasi menawarkan tombol untuk membuka folder berkas. */
  fileId?: string;
}

/** Konfirmasi singkat cukup sekejap; hasil konversi perlu waktu untuk dibaca
 *  dan ditindaklanjuti. */
const DURATION_MS: Record<Toast["tone"], number> = { ok: 12000, bad: 12000, info: 4000 };

interface Props {
  toasts: Toast[];
  onDismiss: (id: string) => void;
  onReveal: (fileId: string) => Promise<void>;
}

export function Toasts({ toasts, onDismiss, onReveal }: Props) {
  return (
    <div className="toasts" aria-live="polite">
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} onDismiss={onDismiss} onReveal={onReveal} />
      ))}
    </div>
  );
}

interface ItemProps {
  toast: Toast;
  onDismiss: (id: string) => void;
  onReveal: (fileId: string) => Promise<void>;
}

function ToastItem({ toast, onDismiss, onReveal }: ItemProps) {
  // Notifikasi yang sedang disorot atau difokus tidak hilang di tengah dibaca.
  const [held, setHeld] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (held) return;
    const timer = setTimeout(() => onDismiss(toast.id), DURATION_MS[toast.tone]);
    return () => clearTimeout(timer);
  }, [held, toast.id, toast.tone, onDismiss]);

  async function reveal() {
    if (!toast.fileId) return;
    setError(null);
    try {
      await onReveal(toast.fileId);
      onDismiss(toast.id);
    } catch (err) {
      setError(messageFor(err, t("history.actionFailed")));
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
        {toast.fileId && (
          <button type="button" className="btn small primary" onClick={() => void reveal()}>
            <FolderIcon />
            {t("history.reveal")}
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

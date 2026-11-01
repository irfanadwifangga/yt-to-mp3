import { useEffect, useRef, type ReactNode } from "react";
import { CloseIcon } from "./icons";
import { t } from "./i18n";

export function Modal({
  title,
  onClose,
  children,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current;
    const previous = document.activeElement as HTMLElement | null;
    dialog?.showModal();
    return () => {
      dialog?.close();
      previous?.focus();
    };
  }, []);
  return (
    <dialog
      ref={ref}
      className="utility-modal"
      aria-label={title}
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}>
      <header className="sheet-head">
        <h2>{title}</h2>
        <button
          type="button"
          className="icon-btn"
          onClick={onClose}
          aria-label={t("settings.close")}>
          <CloseIcon />
        </button>
      </header>
      <div className="utility-body">{children}</div>
    </dialog>
  );
}

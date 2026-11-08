// A modal on the native <dialog>: focus stays inside, Escape closes it, and
// focus returns to the opener.
import {useModalScrollLock} from "../lib/useModalScrollLock";
import { useEffect, useId, useRef, type ReactNode } from "react";

/** While busy (a request is pending) Escape and the close button do nothing. */
export function Dialog({ open, title, onClose, busy = false, children }: { open: boolean; title: string; onClose: () => void; busy?: boolean; children: ReactNode }) {
  useModalScrollLock(open);
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      const opener = document.activeElement as HTMLElement | null;
      d.showModal();
      return () => opener?.focus?.({preventScroll:true});
    }
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog
      ref={ref}
      className="dlg card"
      aria-labelledby={titleId}
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
    >
      {open && (
        <>
          <div className="dlg-h">
            <h2 id={titleId}>{title}</h2>
            <button type="button" className="dlg-x" aria-label="Close" onClick={onClose} disabled={busy}>
              ×
            </button>
          </div>
          {children}
        </>
      )}
    </dialog>
  );
}

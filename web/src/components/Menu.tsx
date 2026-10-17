// A button that opens a menu of choices, one of them current: the theme,
// the account scope and the system switcher. Arrow keys move, Enter or a
// click picks, Escape closes and gives focus back to the button.
import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";

export interface MenuChoice {
  key: string;
  label: ReactNode;
  /** Text after the label, such as a count or an environment. */
  note?: ReactNode;
  icon?: ReactNode;
  checked: boolean;
  onSelect: () => void;
}

/** A command after the choices, such as opening another page. */
export interface MenuAction {
  key: string;
  label: ReactNode;
  onSelect: () => void;
}

interface Props {
  /** Accessible name of the button. */
  label: string;
  /** Accessible name of the menu. */
  menuLabel: string;
  title?: string;
  className?: string;
  /** The button's content. */
  children: ReactNode;
  choices: MenuChoice[];
  align?: "left" | "right";
  actions?: MenuAction[];
}

export function Menu({ label, menuLabel, title, className, children, choices, align = "right", actions = [] }: Props) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const wrap = useRef<HTMLDivElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const button = useRef<HTMLButtonElement>(null);

  const items = () => [...(menu.current?.querySelectorAll<HTMLElement>("[role^=menuitem]") ?? [])];

  useEffect(() => {
    if (!open) return;
    const all = items();
    (all.find((e) => e.getAttribute("aria-checked") === "true") ?? all[0])?.focus();
    const onDown = (e: MouseEvent) => wrap.current && !wrap.current.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [open]);

  const close = () => {
    setOpen(false);
    button.current?.focus();
  };

  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const all = items();
    const at = all.indexOf(document.activeElement as HTMLElement);
    const go = (i: number) => {
      e.preventDefault();
      all[(i + all.length) % all.length]?.focus();
    };
    if (e.key === "Escape") {
      // Handled here: an open drawer around the menu stays open.
      e.preventDefault();
      e.stopPropagation();
      close();
    } else if (e.key === "ArrowDown") go(at + 1);
    else if (e.key === "ArrowUp") go(at - 1);
    else if (e.key === "Home") go(0);
    else if (e.key === "End") go(all.length - 1);
    else if (e.key === "Tab") setOpen(false);
  };

  return (
    <div className="menu-wrap" ref={wrap}>
      <button
        ref={button}
        type="button"
        className={className}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        aria-label={label}
        title={title}
        onClick={() => setOpen((o) => !o)}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" && !open) {
            e.preventDefault();
            setOpen(true);
          }
        }}
      >
        {children}
      </button>
      {open && (
        <div className={`menu card ${align}`} role="menu" id={id} aria-label={menuLabel} ref={menu} onKeyDown={onKey}>
          {choices.map((c) => (
            <button
              key={c.key}
              type="button"
              role="menuitemradio"
              aria-checked={c.checked}
              tabIndex={-1}
              className={c.checked ? "on" : undefined}
              onClick={() => {
                c.onSelect();
                close();
              }}
            >
              {c.icon}
              <span className="ml">{c.label}</span>
              {c.note !== undefined && <span className="mn">{c.note}</span>}
              <span className="mc" aria-hidden="true">
                {c.checked ? "✓" : ""}
              </span>
            </button>
          ))}
          {actions.length > 0 && <div className="menu-sep" role="separator" />}
          {actions.map((a) => (
            <button
              key={a.key}
              type="button"
              role="menuitem"
              tabIndex={-1}
              className="menu-act"
              onClick={() => {
                a.onSelect();
                close();
              }}
            >
              <span className="ml">{a.label}</span>
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

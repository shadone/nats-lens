// The header's time range: preset, span and live freshness, with a panel
// for presets, a custom range and live mode. Times are UTC.
import { serverNow } from "../lib/clock";
import { useEffect, useId, useRef, useState, type FormEvent, type KeyboardEvent } from "react";
import { errorInfo } from "../api/errors";
import type { Freshness } from "../api/model";
import { dayMonHm, fmtDuration, sinceText } from "../lib/format";
import { PRESETS, pauseRange, rangeLabel, resolveRange, resumeLive, type RangeSpec } from "../lib/timeRange";
import { useNow, useTimeRange } from "../lib/useTimeRange";

const FOCUSABLE = "button:not([disabled]), input:not([disabled]), select, textarea, [tabindex]:not([tabindex=\"-1\"])";

/** datetime-local value for a UTC time: 2026-10-17T14:30. */
export function toLocalInput(ms: number): string {
  return new Date(ms).toISOString().slice(0, 16);
}

/** A datetime-local value read as UTC; NaN when invalid. */
export function fromLocalInput(v: string): number {
  return v ? Date.parse(`${v}:00Z`) : NaN;
}

/**
 * Live state in the header: offline when the status call fails (for a fixed
 * range too), fixed for a fixed range, not collecting while the collector's
 * connection to NATS is down or no collector writes, no data without any
 * collection, stale when the newest collection is older than three collect
 * intervals, otherwise live with the collection's age. A narrow header cuts
 * the longer texts with an ellipsis, so their title starts with the whole
 * text; "fixed" and "no data" are short enough to stay whole.
 */
export function Freshness({ spec, freshness, error, id }: { spec: RangeSpec; freshness?: Freshness; error?: unknown; id?: string }) {
  const now = useNow(1000, spec.kind === "live");
  if (error) {
    const i = errorInfo(error);
    return (
      <span className="live offline" id={id} title={`offline: the status call failed: ${i.code}. ${i.hint}`}>
        offline
      </span>
    );
  }
  if (spec.kind === "fixed") return <span className="live paused" id={id}>fixed</span>;
  const newest = freshness?.epochAt !== undefined ? `Newest collection ${freshness.epoch}${freshness.partial ? ", partial" : ""}` : undefined;
  const title = (text: string) => (newest ? `${text}. ${newest}` : text);
  if (freshness && !freshness.collecting) {
    const since = freshness.notCollectingSince;
    const text = `not collecting${since !== undefined ? ` ${sinceText(since, now)}` : ""}`;
    return (
      <span className="live stale" id={id} title={title(text)}>
        {text}
      </span>
    );
  }
  if (!freshness || freshness.epochAt === undefined) return <span className="live stale" id={id}>no data</span>;
  const age = Math.max(0, now - freshness.epochAt);
  const label = !freshness.live ? "not collecting" : age > 3 * freshness.intervalMs ? "stale" : "live";
  return (
    <span className={`live${label === "live" ? "" : " stale"}`} id={id} title={title(`${label} · ${fmtDuration(age)}`)}>
      {label}
      <span className="dotsep"> ·</span> {fmtDuration(age)}
    </span>
  );
}

export function RangePicker({ freshness, error }: { freshness?: Freshness; error?: unknown }) {
  const { spec, setSpec } = useTimeRange();
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const freshId = useId();
  const wrap = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const now = useNow(30_000, spec.kind === "live");
  const r = resolveRange(spec, now);
  const [from, setFrom] = useState(toLocalInput(r.start));
  const [to, setTo] = useState(toLocalInput(r.end));
  const [err, setErr] = useState("");

  useEffect(() => {
    if (!open) return;
    setFrom(toLocalInput(r.start));
    setTo(toLocalInput(r.end));
    setErr("");
    panel.current?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
    const onDown = (e: MouseEvent) => wrap.current && !wrap.current.contains(e.target as Node) && setOpen(false);
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
    // Reset the form only when the panel opens.
  }, [open]);

  /** Closes the panel and gives focus back to the button that opened it. */
  const close = () => {
    setOpen(false);
    trigger.current?.focus();
  };

  /** Escape closes; Tab and Shift+Tab wrap around inside the panel. */
  const onPanelKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "Escape") {
      e.preventDefault();
      close();
      return;
    }
    if (e.key !== "Tab" || !panel.current) return;
    const items = [...panel.current.querySelectorAll<HTMLElement>(FOCUSABLE)];
    const first = items[0];
    const last = items[items.length - 1];
    // After a click on a gap the panel itself holds focus.
    if (document.activeElement === panel.current) {
      e.preventDefault();
      (e.shiftKey ? last : first).focus();
    } else if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  };

  const apply = (e: FormEvent) => {
    e.preventDefault();
    const f = fromLocalInput(from);
    const t = fromLocalInput(to);
    if (!Number.isFinite(f) || !Number.isFinite(t)) return setErr("Enter both times.");
    if (f >= t) return setErr("The start must be before the end.");
    if (t - f > 30 * 86_400_000) return setErr("At most 30 days.");
    if (f > serverNow()) return setErr("The range lies in the future.");
    setSpec({ kind: "fixed", from: f, to: t }, { push: true });
    close();
  };

  return (
    <div className="range-wrap" ref={wrap}>
      <div className="range">
        <button ref={trigger} type="button" className="preset" aria-expanded={open} aria-controls={panelId} onClick={() => setOpen((o) => !o)}>
          {rangeLabel(spec)}
        </button>
        <span className="span" title={`${dayMonHm(r.start)} – ${dayMonHm(r.end)} UTC`}>
          {dayMonHm(r.start)} – {dayMonHm(r.end)}
          <span className="tz"> UTC</span>
        </span>
        <button
          type="button"
          className="live-btn"
          aria-label={spec.kind === "live" ? "Live: pause at the current range" : "Fixed range: go live"}
          aria-describedby={freshId}
          onClick={() => setSpec(spec.kind === "live" ? pauseRange(spec, serverNow()) : resumeLive(spec))}
        >
          <Freshness spec={spec} freshness={freshness} error={error} id={freshId} />
        </button>
      </div>
      {open && (
        <div className="range-panel card" id={panelId} role="dialog" aria-label="Time range" ref={panel} tabIndex={-1} onKeyDown={onPanelKey}>
          <div className="lbl">Live</div>
          <div className="preset-row">
            {PRESETS.map((p) => (
              <button
                key={p.id}
                type="button"
                className={`chip${spec.kind === "live" && spec.preset === p.id ? " on" : ""}`}
                aria-pressed={spec.kind === "live" && spec.preset === p.id}
                onClick={() => {
                  setSpec({ kind: "live", preset: p.id });
                  close();
                }}
              >
                {p.id}
              </button>
            ))}
          </div>
          <form onSubmit={apply} className="custom" noValidate>
            <div className="lbl">Custom, UTC</div>
            <label>
              From
              <input type="datetime-local" value={from} onChange={(e) => setFrom(e.target.value)} required />
            </label>
            <label>
              To
              <input type="datetime-local" value={to} onChange={(e) => setTo(e.target.value)} required />
            </label>
            {err && (
              <p className="field-err" role="alert">
                {err}
              </p>
            )}
            <button type="submit" className="btn p">
              Apply
            </button>
          </form>
        </div>
      )}
    </div>
  );
}

/** Says that the range in the link was not used, and why. */
export function RangeNotice() {
  const { problem, clearRange } = useTimeRange();
  if (!problem) return null;
  return (
    <div className="range-notice" role="status">
      <span>{problem.replace(/\.$/, "")}; showing the last 24 hours instead.</span>
      <button type="button" className="chip" onClick={clearRange}>
        Remove it from the link
      </button>
    </div>
  );
}

// The Replay page's picker of A and B: the range's status strip with its
// Kubernetes windows as buttons, two slider handles that move by keyboard,
// drag or a click on the track, and two UTC inputs.
import { useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Timeline, Window } from "../api/model";
import { dayMon, dayMonHm, hm } from "../lib/format";
import { MAX_SPAN_MS, rangeNotes, windowMoments, type Moments } from "../lib/replayUrl";
import { worstPerBucket } from "../lib/storyLanes";
import { bucketFor, bucketLabel } from "../lib/timeRange";
import { cellRuns, runsText } from "../lib/timelineModel";
import { LEVEL_LABEL } from "../lib/view";
import { cellStyle } from "./StatusTimeline";

const DAY = 86_400_000;
const MINUTE = 60_000;
const SW = 1000;

type Handle = "a" | "b";

/** "Moment A, 09:14 UTC 17 Oct" */
export function handleText(which: Handle, t: number): string {
  return `Moment ${which.toUpperCase()}, ${hm(t)} UTC ${dayMon(t)}`;
}

/** "2026-10-16T14:30" for a datetime-local input, in UTC. */
function inputValue(t: number): string {
  return new Date(t).toISOString().slice(0, 16);
}

/**
 * Moves one handle to t within the rules of GetReplay: A at least one
 * collect interval before B, at most 24 hours apart, B no later than now.
 */
export function moveHandle(m: Moments, which: Handle, t: number, minGapMs: number, now: number): Moments {
  if (which === "a") return { a: Math.min(Math.max(t, m.b - MAX_SPAN_MS), m.b - minGapMs), b: m.b };
  return { a: m.a, b: Math.max(Math.min(t, now, m.a + MAX_SPAN_MS), m.a + minGapMs) };
}

export interface ReplayScrubberProps {
  /** The page's range, the track's ends. */
  start: number;
  end: number;
  live: boolean;
  now: number;
  /** The range's status timeline, every check included; unset while it loads. */
  strip?: Timeline;
  /** Why the strip could not be loaded. */
  stripError?: string;
  moments: Moments;
  minGapMs: number;
  onChange: (m: Moments) => void;
}

export function ReplayScrubber({ start, end, live, now, strip, stripError, moments, minGapMs, onChange }: ReplayScrubberProps) {
  const track = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState<Handle | undefined>(undefined);
  const span = Math.max(1, end - start);
  const frac = (t: number) => Math.min(1, Math.max(0, (t - start) / span));
  const pct = (t: number) => `${(frac(t) * 100).toFixed(3)}%`;
  const step = strip?.bucketMs || bucketFor(span);
  const fmt = span > DAY ? dayMonHm : hm;
  const windows: Window[] = strip?.windows ?? [];

  const timeAt = (clientX: number) => {
    const r = track.current?.getBoundingClientRect();
    if (!r || r.width <= 0) return undefined;
    const t = start + ((clientX - r.left) / r.width) * span;
    return Math.round(t / MINUTE) * MINUTE;
  };
  const move = (which: Handle, t: number) => onChange(moveHandle(moments, which, t, minGapMs, now));

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    const t = timeAt(e.clientX);
    if (t === undefined) return;
    const which: Handle = Math.abs(t - moments.a) <= Math.abs(t - moments.b) ? "a" : "b";
    setDrag(which);
    track.current?.setPointerCapture?.(e.pointerId);
    move(which, t);
    // The handle takes the focus, so the arrow keys go on from here.
    track.current?.querySelector<HTMLElement>(`[data-handle="${which}"]`)?.focus();
    e.preventDefault();
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!drag) return;
    const t = timeAt(e.clientX);
    if (t !== undefined) move(drag, t);
  };
  const endDrag = () => setDrag(undefined);

  const onKey = (which: Handle) => (e: KeyboardEvent<HTMLDivElement>) => {
    const v = moments[which];
    const n = e.shiftKey ? 10 : 1;
    let t: number | undefined;
    switch (e.key) {
      case "ArrowLeft":
      case "ArrowDown":
        t = v - n * step;
        break;
      case "ArrowRight":
      case "ArrowUp":
        t = v + n * step;
        break;
      case "PageDown":
        t = v - 10 * step;
        break;
      case "PageUp":
        t = v + 10 * step;
        break;
      case "Home":
        t = which === "a" ? start : -Infinity;
        break;
      case "End":
        t = which === "a" ? Infinity : Math.min(end, now);
        break;
      default:
        return;
    }
    e.preventDefault();
    move(which, t);
  };

  const cells = strip ? worstPerBucket(strip.rows, strip.bucketCount) : [];
  const runs = strip ? runsText(cellRuns(cells, strip.start, strip.bucketMs), fmt) : "";
  const notes = rangeNotes(moments, { start, end });

  return (
    <div className="card rs">
      {windows.length > 0 && (
      <div className="rs-wins" aria-label="Kubernetes windows" role="group">
        {windows.map((w) => (
          <button
            key={`${w.start}-${w.end}`}
            type="button"
            className="rs-win"
            style={{ left: pct(w.start), width: `max(8px, calc(${pct(w.end)} - ${pct(w.start)}))` }}
            aria-label={`Replay ${w.label} ${hm(w.start)}–${hm(w.end)}`}
            title={`${w.label} ${hm(w.start)}–${hm(w.end)} UTC${w.detail ? `: ${w.detail}` : ""}`}
            onClick={() => onChange(windowMoments(w, now))}
          >
            <span>{w.label}</span>
          </button>
        ))}
      </div>
      )}
      <div
        ref={track}
        className={`rs-track${drag ? " dragging" : ""}`}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
      >
        <svg className="rs-strip" viewBox={`0 0 ${SW} 18`} preserveAspectRatio="none" role="img" aria-label={strip ? `Status per ${bucketLabel(strip.bucketMs)}: ${runs}` : "Status strip loading"}>
          {strip &&
            cells.map((lv, i) => {
              const t0 = strip.start + i * strip.bucketMs;
              const x = frac(t0) * SW;
              const w = (strip.bucketMs / span) * SW;
              return (
                <rect key={i} x={(x + 0.6).toFixed(2)} y="0" width={Math.max(0.5, w - 1.6).toFixed(2)} height="18" rx="1.5" style={cellStyle(lv)} vectorEffect="non-scaling-stroke" data-level={lv}>
                  <title>{`${fmt(t0)} ${LEVEL_LABEL[lv].toLowerCase()}`}</title>
                </rect>
              );
            })}
        </svg>
        {windows.map((w) => (
          <span key={`${w.start}-${w.end}`} className="rs-wshade" style={{ left: pct(w.start), width: `calc(${pct(w.end)} - ${pct(w.start)})` }} aria-hidden="true" />
        ))}
        <span className="rs-sel" style={{ left: pct(moments.a), width: `calc(${pct(moments.b)} - ${pct(moments.a)})` }} aria-hidden="true" />
        {(["a", "b"] as const).map((which) => {
          const v = moments[which];
          return (
            <div
              key={which}
              data-handle={which}
              className={`rs-handle rs-${which}`}
              style={{ left: pct(v) }}
              role="slider"
              tabIndex={0}
              aria-label={`Moment ${which.toUpperCase()}`}
              aria-valuemin={Math.min(start, v)}
              aria-valuemax={Math.max(end, v)}
              aria-valuenow={v}
              aria-valuetext={handleText(which, v)}
              onKeyDown={onKey(which)}
            >
              <span aria-hidden="true">{which.toUpperCase()}</span>
            </div>
          );
        })}
      </div>
      <div className="rs-axis" aria-hidden="true">
        <span>{fmt(start)}</span>
        <span>{live ? "now" : fmt(end)}</span>
      </div>
      {stripError && <p className="rs-note">The status strip could not be loaded: {stripError}</p>}
      {notes.length > 0 && (
        <p className="rs-note" role="note">
          {notes.join(" ")} Replay still reads it.
        </p>
      )}
      <div className="rs-inputs">
        {(["a", "b"] as const).map((which) => (
          <label key={which} className="grp">
            <span className="lbl">{which.toUpperCase()} (UTC)</span>
            <input
              type="datetime-local"
              aria-label={`${which.toUpperCase()} (UTC)`}
              value={inputValue(moments[which])}
              max={inputValue(now)}
              onChange={(e) => {
                const t = Date.parse(`${e.target.value}:00Z`);
                if (e.target.value && !Number.isNaN(t)) onChange({ ...moments, [which]: t });
              }}
            />
          </label>
        ))}
        <span className="rs-hint">Arrows move a handle by {bucketLabel(step)}, Shift+arrows by ten.</span>
      </div>
    </div>
  );
}

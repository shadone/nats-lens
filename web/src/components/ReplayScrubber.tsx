import {InfoTip} from "./MetricBox";
// The Replay page's picker of A and B: the range's status strip, its
// Kubernetes windows as a bar of buttons under it that set A and B, two
// handles that move by keyboard, drag or a click on the track, an axis,
// and two UTC inputs. A moving handle is drawn from local state; the
// moments go to the page once it rests (pointer up, key up, blur), so the
// URL is written once per move.
import { useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Level, Timeline, Window } from "../api/model";
import { dayMon, dayMonHm, hm } from "../lib/format";
import { MAX_SPAN_MS, clampMoments, rangeNotes, windowMoments, type Moments } from "../lib/replayUrl";
import { worstPerBucket } from "../lib/storyLanes";
import { bucketFor, bucketLabel } from "../lib/timeRange";
import { axisTicks, cellRuns, runsText } from "../lib/timelineModel";
import { useWidth } from "../lib/useWidth";
import { LEVEL_LABEL } from "../lib/view";
import { cellStyle } from "./StatusTimeline";

const DAY = 86_400_000;
/** The keys that move a handle; letting one go applies the move. */
const MOVE_KEYS = new Set(["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "PageUp", "PageDown", "Home", "End"]);
const MINUTE = 60_000;
const SW = 1000;

type Handle = "a" | "b";

const toSecond = (t: number) => Math.floor(t / 1000) * 1000;

/** "Moment A, 09:14 UTC 17 Oct" */
export function handleText(which: Handle, t: number): string {
  return `Moment ${which.toUpperCase()}, ${hm(t)} UTC ${dayMon(t)}`;
}

/** "2026-10-16T14:30" for a datetime-local input, in UTC. */
function inputValue(t: number): string {
  return new Date(t).toISOString().slice(0, 16);
}

/**
 * The strip's state of a bucket: info counts as ok, as Fleet counts a
 * system whose only findings are info as OK, so the strip shows trouble.
 */
export function stripLevel(l: Level): Level {
  return l === "info" ? "ok" : l;
}

/**
 * Moves one handle to t within the rules of GetReplay: A at least one
 * collect interval before B, at most 24 hours apart, B no later than now.
 */
export function moveHandle(m: Moments, which: Handle, t: number, minGapMs: number, now: number): Moments {
  if (which === "a") return { a: Math.min(Math.max(t, m.b - MAX_SPAN_MS), m.b - minGapMs), b: m.b };
  return { a: m.a, b: Math.max(Math.min(t, now, m.a + MAX_SPAN_MS), m.a + minGapMs) };
}

/** A UTC input that keeps what is typed until Enter or leaving the field, so a half-typed date is never applied. */
function MomentInput({ which, value, max, onCommit }: { which: Handle; value: number; max: number; onCommit: (t: number) => void }) {
  const [draft, setDraft] = useState<string | undefined>(undefined);
  const commit = () => {
    if (draft === undefined) return;
    setDraft(undefined);
    const t = Date.parse(`${draft}:00Z`);
    if (draft && !Number.isNaN(t) && t !== value) onCommit(t);
  };
  const label = `${which.toUpperCase()} (UTC)`;
  return (
    <label className="grp">
      <span className="lbl">{label}</span>
      <input
        type="datetime-local"
        aria-label={label}
        value={draft ?? inputValue(value)}
        max={inputValue(max)}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter") commit();
          if (e.key === "Escape") setDraft(undefined);
        }}
      />
    </label>
  );
}

/** Width of a handle's chip: "A 08:50", or the letter alone on a narrow track. */
const CHIP_PX = 68;
const CHIP_NARROW_PX = 26;
/** Below this track width the chips show the letter alone. */
const NARROW_TRACK_PX = 480;

/**
 * Where the two chips start, in pixels: A's ends at its line and B's
 * starts at its line, both inside the track, and they never overlap; when
 * the lines are closer than a chip near an end, the chips move apart.
 */
export function chipLefts(pa: number, pb: number, width: number, chip: number): { a: number; b: number } {
  let a = Math.max(0, pa - chip);
  let b = Math.min(width - chip, pb);
  if (b < a + chip) {
    if (a + 2 * chip <= width) b = a + chip;
    else {
      b = width - chip;
      a = Math.max(0, width - 2 * chip);
    }
  }
  return { a, b };
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
  const width = useWidth(track);
  const [drag, setDrag] = useState<Handle | undefined>(undefined);
  // The moments while a handle moves; the page hears of them when it rests.
  const [draft, setDraft] = useState<Moments | undefined>(undefined);
  const draftRef = useRef<Moments | undefined>(undefined);
  // The moments last sent to the page, ahead of the props until the page
  // renders them: a typed moment applied on blur by a press on the track
  // is the base of the drag that press starts. Sent moments are clamped
  // and cut to the second as the page reads them, so they are what the
  // page will show; a change of the page's moments replaces them.
  const sent = useRef<Moments>(moments);
  // Only a change of the moments the page shows resets it: a render of a
  // pending move before the page's transition commits must not.
  useLayoutEffect(() => {
    sent.current = moments;
  }, [moments.a, moments.b]);
  const send = (m: Moments) => {
    const c = clampMoments(m, now, minGapMs);
    const n = { a: toSecond(c.a), b: toSecond(c.b) };
    if (n.a === sent.current.a && n.b === sent.current.b) return;
    sent.current = n;
    onChange(n);
  };
  const shown = draft ?? moments;
  const span = Math.max(1, end - start);
  const frac = (t: number) => Math.min(1, Math.max(0, (t - start) / span));
  const pct = (t: number) => `${(frac(t) * 100).toFixed(3)}%`;
  const step = strip?.bucketMs || bucketFor(span);
  const fmt = span > DAY ? dayMonHm : hm;
  const windows: Window[] = strip?.windows ?? [];
  // A window's time carries the day when it is not on A's day.
  const windowTime = (t: number) => (dayMon(t) === dayMon(moments.a) ? hm(t) : dayMonHm(t));

  const preview = (m: Moments) => {
    draftRef.current = m;
    setDraft(m);
  };
  const commit = () => {
    const m = draftRef.current;
    draftRef.current = undefined;
    setDraft(undefined);
    if (m) send(m);
  };
  /** Drops a moving handle's moments: it goes back to where it was. */
  const discard = () => {
    draftRef.current = undefined;
    setDraft(undefined);
  };
  const move = (which: Handle, t: number) => preview(moveHandle(draftRef.current ?? sent.current, which, t, minGapMs, now));

  const timeAt = (clientX: number) => {
    const r = track.current?.getBoundingClientRect();
    if (!r || r.width <= 0) return undefined;
    const t = start + ((clientX - r.left) / r.width) * span;
    return Math.round(t / MINUTE) * MINUTE;
  };

  const grab = (which: Handle, e: PointerEvent<HTMLElement>) => {
    setDrag(which);
    track.current?.setPointerCapture?.(e.pointerId);
    // The handle takes the focus, so the arrow keys go on from here.
    track.current?.querySelector<HTMLElement>(`[data-handle="${which}"]`)?.focus();
  };
  const onTrackDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    const t = timeAt(e.clientX);
    if (t === undefined) return;
    const which: Handle = Math.abs(t - shown.a) <= Math.abs(t - shown.b) ? "a" : "b";
    grab(which, e);
    move(which, t);
    e.preventDefault();
  };
  const onHandleDown = (which: Handle) => (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.preventDefault();
    grab(which, e);
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    if (!drag) return;
    const t = timeAt(e.clientX);
    if (t !== undefined) move(drag, t);
  };
  const endDrag = () => {
    if (!drag) return;
    setDrag(undefined);
    commit();
  };
  const cancelDrag = () => {
    if (!drag) return;
    setDrag(undefined);
    discard();
  };

  const onKey = (which: Handle) => (e: KeyboardEvent<HTMLDivElement>) => {
    const v = (draftRef.current ?? sent.current)[which];
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

  // Info is folded into ok in each row before the worst is taken, so a
  // bucket with an info and a muted check shows muted.
  const cells = strip ? worstPerBucket(strip.rows.map((r) => ({ ...r, cells: r.cells.map(stripLevel) })), strip.bucketCount) : [];
  const runs = strip ? runsText(cellRuns(cells, strip.start, strip.bucketMs), fmt) : "";
  const notes = rangeNotes(shown, { start, end });
  const ticks = width ? axisTicks(start, end, live, width) : [];
  const narrow = width > 0 && width < NARROW_TRACK_PX;
  const chip = narrow ? CHIP_NARROW_PX : CHIP_PX;
  const lefts = width ? chipLefts(frac(shown.a) * width, frac(shown.b) * width, width, chip) : undefined;

  return (
    <div className="card rs">
      <div
        ref={track}
        className={`rs-track${drag ? " dragging" : ""}${narrow ? " narrow" : ""}${windows.length ? " has-wins" : ""}`}
        onPointerDown={onTrackDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={cancelDrag}
      >
        <svg className="rs-strip" viewBox={`0 0 ${SW} 26`} preserveAspectRatio="none" role="img" aria-label={strip ? `Status per ${bucketLabel(strip.bucketMs)}: ${runs}` : "Status strip loading"}>
          {strip &&
            cells.map((lv, i) => {
              const t0 = strip.start + i * strip.bucketMs;
              const x = frac(t0) * SW;
              const w = (strip.bucketMs / span) * SW;
              return (
                <rect key={i} x={(x + 0.6).toFixed(2)} y="0" width={Math.max(0.5, w - 1.6).toFixed(2)} height="26" rx="2" style={cellStyle(lv)} vectorEffect="non-scaling-stroke" data-level={lv}>
                  <title>{`${fmt(t0)} ${LEVEL_LABEL[lv].toLowerCase()}`}</title>
                </rect>
              );
            })}
        </svg>
        {windows.length > 0 && (
          <div className="rs-wins" role="group" aria-label="Kubernetes windows">
            {windows.map((w) => (
              <button
                key={`${w.start}-${w.end}`}
                type="button"
                className={`rs-win${frac(w.start) > 0.6 ? " end" : ""}`}
                style={{ left: pct(w.start), width: `calc(${pct(w.end)} - ${pct(w.start)})` }}
                aria-label={`Replay ${w.label} ${windowTime(w.start)}–${windowTime(w.end)} UTC`}
                title={`${w.label} ${windowTime(w.start)}–${windowTime(w.end)} UTC${w.detail ? `: ${w.detail}` : ""}`}
                onPointerDown={(e) => e.stopPropagation()}
                onClick={() => send(windowMoments(w, now))}
              >
                <span className="rs-wext" aria-hidden="true" />
                <span className="rs-wl">{w.label}</span>
              </button>
            ))}
          </div>
        )}
        {(["a", "b"] as const).map((which) => {
          const v = shown[which];
          const p = pct(v);
          return (
            <span key={which}>
              <span className={`rs-line rs-line-${which}`} style={{ left: p }} aria-hidden="true" />
              <div
                data-handle={which}
                className={`rs-chip rs-${which}`}
                style={{ left: lefts ? `${lefts[which]}px` : which === "a" ? `max(0px, calc(${p} - ${chip}px))` : `min(${p}, calc(100% - ${chip}px))`, width: chip }}
                role="slider"
                tabIndex={0}
                aria-label={`Moment ${which.toUpperCase()}`}
                aria-valuemin={Math.min(start, v)}
                aria-valuemax={Math.max(end, v)}
                aria-valuenow={v}
                aria-valuetext={handleText(which, v)}
                onKeyDown={onKey(which)}
                onKeyUp={(e) => MOVE_KEYS.has(e.key) && commit()}
                onBlur={commit}
                onPointerDown={onHandleDown(which)}
              >
                {which.toUpperCase()}
                {!narrow && <span className="rs-ht"> {hm(v)}</span>}
              </div>
            </span>
          );
        })}
      </div>
      <div className="rs-axis" aria-hidden="true">
        {ticks.map((t) => (
          <span key={`${t.label}-${t.frac}`} className={t.edge ? "edge" : undefined} style={t.edge ? undefined : { left: `${(t.frac * 100).toFixed(3)}%` }}>
            {t.label}
          </span>
        ))}
      </div>
      {stripError && <p className="rs-note">The status strip could not be loaded: {stripError}</p>}
      {notes.length > 0 && (
        <p className="rs-note" role="note">
          {notes.join(" ")} Replay still reads it.
        </p>
      )}
      <div className="rs-inputs">
        {(["a", "b"] as const).map((which) => (
          <MomentInput key={which} which={which} value={moments[which]} max={now} onCommit={(t) => send(moveHandle(sent.current, which, t, minGapMs, now))} />
        ))}
        <InfoTip label="Replay time strip" text="Choose two observation times, A and B, to compare captured evidence. The coloured strip summarizes finding states per bucket; info-only states appear as OK. Shaded windows are inferred context, not proven causes. Missing captures stay unknown; this is not a recording of every change between the selected times."/>
        <span className="rs-hint">Arrows move a handle by {bucketLabel(step)}, Shift+arrows by ten.</span>
      </div>
    </div>
  );
}

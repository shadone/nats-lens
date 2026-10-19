// The Replay page's two moments in the URL: ?a=<ISO>&b=<ISO>, beside the
// global range and system. Parsing, the defaults for a link without them,
// and the clamping to what GetReplay accepts: A before B, at most 24 hours
// apart, B no later than now.
import type { Window } from "../api/model";
import { dayMonHm } from "./format";
import { pauseRange, writeRange, type RangeSpec } from "./timeRange";

export const A_PARAM = "a";
export const B_PARAM = "b";

/** The longest span GetReplay reads. */
export const MAX_SPAN_MS = 24 * 3_600_000;
/** The span of the default without a Kubernetes window: the last hour of the range. */
export const DEFAULT_SPAN_MS = 3_600_000;
/** How far a Kubernetes window is widened on each side when it sets A and B. */
export const WINDOW_PAD_MS = 5 * 60_000;

/** A UTC time with a Z, to the minute or finer, as the range's from and to. */
const ISO = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?Z$/;

/** A UTC time of the link; undefined for any other text, and for a day that does not exist, such as 30 Feb. */
export function parseMoment(v: string | null): number | undefined {
  if (!v || !ISO.test(v)) return undefined;
  const t = Date.parse(v);
  if (Number.isNaN(t) || new Date(t).toISOString().slice(0, 16) !== v.slice(0, 16)) return undefined;
  return t;
}

/** 2026-10-19T09:14:00Z, to the second. */
export function momentIso(ms: number): string {
  return new Date(Math.floor(ms / 1000) * 1000).toISOString().replace(".000Z", "Z");
}

/** "17 Oct 09:14 UTC" */
export function momentText(ms: number): string {
  return `${dayMonHm(ms)} UTC`;
}

export interface Moments {
  a: number;
  b: number;
}

/** A Kubernetes window widened by five minutes on each side, B no later than now. */
export function windowMoments(w: Window, now: number): Moments {
  return { a: w.start - WINDOW_PAD_MS, b: Math.min(w.end + WINDOW_PAD_MS, now) };
}

/** The newest Kubernetes window that overlaps the range, else the last hour of the range. */
export function defaultMoments(windows: Window[], range: { start: number; end: number }, now: number): Moments {
  const inRange = windows.filter((w) => w.end >= range.start && w.start <= range.end);
  const newest = inRange.reduce<Window | undefined>((best, w) => (!best || w.start > best.start ? w : best), undefined);
  if (newest) return windowMoments(newest, now);
  const end = Math.min(range.end, now);
  return { a: end - DEFAULT_SPAN_MS, b: end };
}

/**
 * The moments GetReplay accepts, with a note for each change: A before B
 * (swapped when after it), B no later than now, A before now (an hour
 * before B when it is not), at least minGapMs apart and at most 24 hours
 * apart (A moves).
 */
export function clampMoments(m: Moments, now: number, minGapMs: number): Moments & { notes: string[] } {
  let { a, b } = m;
  const notes: string[] = [];
  if (a > b) {
    [a, b] = [b, a];
    notes.push("A was after B, so the two were swapped.");
  }
  const aFuture = a >= now;
  if (b > now) {
    b = now;
    notes.push("B is after now, so it is read as now.");
  }
  if (aFuture) {
    a = b - DEFAULT_SPAN_MS;
    notes.push("A is at or after now, so it is read as an hour before now.");
  }
  if (b - a < minGapMs) {
    a = b - minGapMs;
    notes.push("A must be at least one collect interval before B, so A was moved.");
  }
  if (b - a > MAX_SPAN_MS) {
    a = b - MAX_SPAN_MS;
    notes.push("Replay reads at most 24 hours, so A was moved to 24 hours before B.");
  }
  return { a, b, notes };
}

export interface Resolved extends Moments {
  /** Why the moments differ from the link's. */
  notes: string[];
  /** The link does not say these moments (it lacks one, or they were clamped): write them with replace. */
  write: boolean;
}

/**
 * The moments of a link. A link without one, or with one that is no valid
 * time, takes a default: an hour from the moment it has, else the newest
 * Kubernetes window of the range (undefined while the windows are not
 * known yet), else the last hour of the range.
 */
export function resolveMoments(
  rawA: string | null,
  rawB: string | null,
  ctx: { windows: Window[] | undefined; range: { start: number; end: number }; now: number; minGapMs: number },
): Resolved | undefined {
  const pa = parseMoment(rawA);
  const pb = parseMoment(rawB);
  const notes: string[] = [];
  if (rawA && pa === undefined) notes.push(`The link's A "${rawA}" is not a valid UTC time, so a default was used.`);
  if (rawB && pb === undefined) notes.push(`The link's B "${rawB}" is not a valid UTC time, so a default was used.`);
  let m: Moments;
  if (pa !== undefined && pb !== undefined) m = { a: pa, b: pb };
  else if (pa !== undefined && pa >= ctx.now) {
    notes.push("The link's A is after now, so the hour before now is shown.");
    m = { a: ctx.now - DEFAULT_SPAN_MS, b: ctx.now };
  } else if (pa !== undefined) m = { a: pa, b: Math.min(pa + DEFAULT_SPAN_MS, ctx.now) };
  else if (pb !== undefined && pb > ctx.now) {
    notes.push("The link's B is after now, so the hour before now is shown.");
    m = { a: ctx.now - DEFAULT_SPAN_MS, b: ctx.now };
  } else if (pb !== undefined) m = { a: pb - DEFAULT_SPAN_MS, b: pb };
  else if (ctx.windows === undefined) return undefined;
  else m = defaultMoments(ctx.windows, ctx.range, ctx.now);
  const c = clampMoments(m, ctx.now, ctx.minGapMs);
  // To the second, as the link writes them, so a written default reads back the same.
  const sec = (t: number) => Math.floor(t / 1000) * 1000;
  const a = sec(c.a);
  const b = sec(c.b);
  return { a, b, notes: [...notes, ...c.notes], write: momentIso(a) !== rawA || momentIso(b) !== rawB };
}

/** What the scrubber says about a moment outside the page's range. */
export function rangeNotes(m: Moments, range: { start: number; end: number }): string[] {
  const where = (t: number) => (t < range.start ? "before" : t > range.end ? "after" : undefined);
  const wa = where(m.a);
  const wb = where(m.b);
  if (wa && wa === wb) return [`A and B are ${wa} this range.`];
  return [wa && `A is ${wa} this range.`, wb && `B is ${wb} this range.`].filter((x): x is string => !!x);
}

/** Params with the moments written into them; other parameters stay. */
export function writeMoments(params: URLSearchParams, m: Moments): URLSearchParams {
  const out = new URLSearchParams(params);
  out.set(A_PARAM, momentIso(m.a));
  out.set(B_PARAM, momentIso(m.b));
  return out;
}

/**
 * The query of a link to this A–B that shows the next person the same
 * page: a live range becomes fixed at now, so the strip does not move on.
 */
export function shareParams(params: URLSearchParams, spec: RangeSpec, m: Moments, now: number): URLSearchParams {
  return writeMoments(writeRange(params, pauseRange(spec, now)), m);
}

/** "20261019T091400Z", for a file name. */
export function compactMoment(ms: number): string {
  return momentIso(ms).replace(/[-:]/g, "");
}

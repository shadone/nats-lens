// What the other pages say about the history behind them: the note under
// the header when the range starts before the history, the hatched gaps
// on charts, how long a growth rate still needs, the findings already
// true when the history begins, and the span behind a tile. Apart from
// React so they can be tested on their own; the Data history page's own
// words are in historyView.ts.
import type { HistoryGap } from "../api/history";
import type { Finding } from "../api/model";
import { rangeText } from "../charts/chartLabel";
import { followsNow, gapName, spanText, stripSegments, whenText } from "./historyView";
import type { RangeSpec } from "./timeRange";

const MIN = 60_000;
const HOUR = 60 * MIN;

/** The history a growth rate needs in the range: api.MinGrowthSpan. */
export const GROWTH_NEEDS_MS = 6 * HOUR;

/** How far after its start a finding still counts as first seen at the history start. */
const START_SLACK_MS = MIN;

/** A range as a page shows it, with where the history begins. */
export interface HistoryFrame {
  historyStart?: number;
  start: number;
  end: number;
  live: boolean;
  now: number;
  /** When the collector stopped collecting; undefined while it collects. */
  stoppedAt?: number;
  /** The newest collection's time while the collector does not collect. */
  lastDataAt?: number;
}

/** The history inside the range; 0 when it begins at or after the range's end, undefined without a history. */
export function historyInRange(f: Pick<HistoryFrame, "historyStart" | "start" | "end">): number | undefined {
  if (f.historyStart === undefined) return undefined;
  return Math.max(0, f.end - Math.max(f.start, f.historyStart));
}

const PRESET_WORDS: Record<string, string> = {
  "1h": "the last hour",
  "6h": "the last 6 h",
  "24h": "the last 24 h",
  "7d": "the last 7 days",
};

/** "the last 24 h", or the fixed range's ends. */
export function askedText(spec: RangeSpec, start: number, end: number): string {
  return spec.kind === "live" ? PRESET_WORDS[spec.preset] : rangeText(start, end);
}

/**
 * The line under the header when the range starts before the history by
 * more than an interval: "31 min of history: you asked for the last 24 h;
 * the history begins at 14:29 UTC." Undefined when the history covers the
 * range or there is none.
 */
export function historyNoteText(f: HistoryFrame, spec: RangeSpec, intervalMs: number): string | undefined {
  if (f.historyStart === undefined || f.start >= f.historyStart - intervalMs) return undefined;
  const has = historyInRange(f) ?? 0;
  const asked = askedText(spec, f.start, f.end);
  const begins = `the history begins at ${whenText(f.historyStart, f.now)}`;
  if (has === 0) return `No history in this range: you asked for ${asked}; ${begins}.`;
  return `${spanText(has)} of history: you asked for ${asked}; ${begins}.`;
}

/**
 * Why a growth rate is not there yet, when the range holds less history
 * than it needs: "needs 6 h of history; 5 h 29 min to go" on a range that
 * follows now, "this range holds 31 min" on one that ended, "this range
 * is 1 h" when the range itself is too short, "has 5 h 59 min, not
 * collecting since 20:27 UTC" while the collector has stopped, as no
 * time to go is known then. Undefined when the range has enough history,
 * or none: a rate missing then has another reason.
 */
export function growthWaitText(f: HistoryFrame): string | undefined {
  const stopped = f.stoppedAt !== undefined && f.stoppedAt < f.end;
  const dataEnd = stopped ? Math.min(f.end, f.lastDataAt ?? f.stoppedAt!) : f.end;
  const has = historyInRange({ ...f, end: dataEnd });
  if (has === undefined || has >= GROWTH_NEEDS_MS) return undefined;
  const needs = `needs ${spanText(GROWTH_NEEDS_MS)} of history`;
  if (f.end - f.start < GROWTH_NEEDS_MS) return `${needs}; this range is ${spanText(f.end - f.start)}`;
  if (stopped) return `${needs}; has ${spanText(has)}, not collecting since ${whenText(f.stoppedAt!, f.now)}`;
  if (followsNow(f.end, f.live, f.now)) return `${needs}; ${spanText(GROWTH_NEEDS_MS - has)} to go`;
  return `${needs}; this range holds ${spanText(has)}`;
}

/** "over 31 min", for a tile whose range holds less history than it spans; undefined otherwise. */
export function tileSpanText(f: Pick<HistoryFrame, "historyStart" | "start" | "end">, intervalMs: number): string | undefined {
  const has = historyInRange(f);
  if (has === undefined || f.historyStart === undefined || f.start >= f.historyStart - intervalMs) return undefined;
  return has === 0 ? "no history in this range" : `over ${spanText(has)}`;
}

/**
 * A firing finding whose condition was already true when the history
 * begins: its condition began before the oldest stored collection, or it
 * was first seen within a minute after it.
 */
export function atHistoryStart(f: Pick<Finding, "since" | "conditionSince" | "state">, historyStart: number | undefined): boolean {
  if (historyStart === undefined || f.since === undefined || f.state === "resolved") return false;
  if (f.conditionSince !== undefined && f.conditionSince < historyStart) return true;
  return f.since >= historyStart && f.since - historyStart < START_SLACK_MS;
}

/** "Already true when the history begins, at 13:47 UTC." for one finding, "3 of 4 were …" for a group. */
export function atStartText(n: number, of: number, historyStart: number, now: number): string {
  const at = whenText(historyStart, now);
  if (of === 1) return `Already true when the history begins, at ${at}.`;
  return `${n === of ? `All ${of}` : `${n} of ${of}`} were already true when the history begins, at ${at}.`;
}

/** The hatched gaps a page's charts draw, from its Data history answer. */
export interface ChartGaps {
  /** Only the kinds charts hatch. */
  gaps: HistoryGap[];
  /** The end of the range the answer describes, for the gaps' names. */
  rangeEnd: number;
  live: boolean;
}

export interface GapMark {
  key: string;
  /** Percent of the chart's span from its start. */
  left: number;
  width: number;
  /** "No data 09:40:12–09:43:12 UTC: nats-lens was not running" */
  name: string;
}

/** The hatched gaps placed on a chart over [start, end]; a gap outside it draws nothing. */
export function gapMarks(g: ChartGaps | undefined, start: number, end: number, now: number): GapMark[] {
  if (!g || g.gaps.length === 0) return [];
  return stripSegments({ gaps: g.gaps, rangeStart: start, rangeEnd: end })
    .filter((s) => s.width > 0)
    .map((s) => ({
      key: `${s.gap.kind}|${s.gap.start ?? ""}|${s.gap.eventId}`,
      left: s.left,
      width: s.width,
      name: gapName(s.gap, g.rangeEnd, g.live, now),
    }));
}

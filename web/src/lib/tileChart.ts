// Whether an Overview tile draws its chart. A steady tile shows its number
// and "steady at N" without a chart; anything that is not known to be steady
// keeps the chart, so the UI never shows silence as health.
import { HATCHED_KINDS, type HistoryGap } from "../api/history";
import type { Level, Point } from "../api/model";

export interface TileChartFacts {
  points: Point[];
  points2?: Point[];
  stepMs?: number;
  start: number;
  end: number;
  status: Level;
  /** The series counts changes per bucket (the meta leader): any non-zero bucket is a change. */
  changesSeries?: boolean;
  /** The series' own min and max; the newest report can widen them past the buckets. */
  min?: number;
  max?: number;
  /** A live range, whose newest bucket may not hold a collection yet. */
  live?: boolean;
  /** When nats-lens began collecting; undefined when not known. */
  historyStart?: number;
  /** The range's collection gaps; undefined while they are not known. */
  gaps?: readonly HistoryGap[];
}

export type TileChartReason = "trouble" | "noData" | "gap" | "gapsUnknown" | "changed" | "steady";

/**
 * Collection gaps a chart hatches inside the range, and gaps of a kind this
 * UI does not know; time before the history is said in words.
 */
const GAP_KINDS: readonly HistoryGap["kind"][] = [...HATCHED_KINDS.filter((k) => k !== "beforeHistory"), "unknown"];

/**
 * Whether a bucket of the range has no point. Buckets lie on a grid of the
 * step from the Unix epoch, so the first may begin before the range.
 */
function missingBuckets(p: Point[], f: TileChartFacts): boolean {
  const step = f.stepMs ?? 0;
  if (!step) return true;
  // A series may begin with the history; earlier buckets are missing ones.
  const first = Math.floor(Math.max(f.start, f.historyStart ?? f.start) / step) * step;
  if (p[0].time > first) return true;
  for (let i = 1; i < p.length; i++) if (p[i].time - p[i - 1].time > 1.5 * step) return true;
  const last = Math.floor((f.end - 1) / step) * step;
  return p[p.length - 1].time < last - (f.live ? step : 0);
}

function collectionGap(f: TileChartFacts): boolean {
  return (f.gaps ?? []).some((g) => GAP_KINDS.includes(g.kind) && Math.max(f.start, g.start ?? f.start) < Math.min(f.end, g.end ?? f.end));
}

/** A count bucket's low and high hold its lowest and highest collection. */
function varies(points: Point[] | undefined, changes: boolean): boolean {
  if (!points?.length) return false;
  if (changes) return points.some((p) => p.value !== 0);
  const v = points[0].value;
  return points.some((p) => p.value !== v || (p.low !== undefined && p.low !== v) || (p.high !== undefined && p.high !== v));
}

export function tileChartReason(f: TileChartFacts): TileChartReason {
  if (f.status === "critical" || f.status === "warning") return "trouble";
  if (f.points.length === 0) return "noData";
  if (missingBuckets(f.points, f) || (f.points2?.length && missingBuckets(f.points2, f)) || collectionGap(f)) return "gap";
  if (varies(f.points, !!f.changesSeries) || varies(f.points2, !!f.changesSeries)) return "changed";
  if (f.min !== undefined && f.max !== undefined && f.min !== f.max) return "changed";
  if (f.gaps === undefined) return "gapsUnknown";
  return "steady";
}

/** Indexes of the points whose value differs from the point before. */
export function changedSegments(points: Point[]): number[] {
  const out: number[] = [];
  for (let i = 1; i < points.length; i++) if (points[i].value !== points[i - 1].value) out.push(i);
  return out;
}

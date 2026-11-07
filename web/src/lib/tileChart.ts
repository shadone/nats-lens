// Whether an Overview tile draws its chart. A steady tile shows its number
// and "steady at N" without a chart; anything that is not known to be steady
// keeps the chart, so the UI never shows silence as health.
import type { HistoryGap } from "../api/history";
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
  /** When nats-lens began collecting; undefined when not known. */
  historyStart?: number;
  /** The range's collection gaps; undefined while they are not known. */
  gaps?: readonly HistoryGap[];
}

export type TileChartReason = "trouble" | "noData" | "gap" | "gapsUnknown" | "changed" | "steady";

/** Collection gaps a chart hatches inside the range; time before the history is said in words. */
const GAP_KINDS: readonly HistoryGap["kind"][] = ["notRunning", "notConnected", "noCollections"];

function missingBuckets(f: TileChartFacts): boolean {
  const step = f.stepMs ?? 0;
  if (!step) return true;
  const p = f.points;
  // A series may begin with the history; earlier buckets are missing ones.
  const from = Math.max(f.start, f.historyStart ?? f.start);
  if (p[0].time - from > 1.5 * step) return true;
  for (let i = 1; i < p.length; i++) if (p[i].time - p[i - 1].time > 1.5 * step) return true;
  // The newest bucket may not hold a collection yet; two steps allow for it.
  return f.end - p[p.length - 1].time > 2 * step;
}

function collectionGap(f: TileChartFacts): boolean {
  return (f.gaps ?? []).some((g) => GAP_KINDS.includes(g.kind) && Math.max(f.start, g.start ?? f.start) < Math.min(f.end, g.end ?? f.end));
}

function varies(points: Point[] | undefined, changes: boolean): boolean {
  if (!points?.length) return false;
  return changes ? points.some((p) => p.value !== 0) : points.some((p) => p.value !== points[0].value);
}

export function tileChartReason(f: TileChartFacts): TileChartReason {
  if (f.status === "critical" || f.status === "warning") return "trouble";
  if (f.points.length === 0) return "noData";
  if (missingBuckets(f) || collectionGap(f)) return "gap";
  if (varies(f.points, !!f.changesSeries) || varies(f.points2, !!f.changesSeries)) return "changed";
  if (f.gaps === undefined) return "gapsUnknown";
  return "steady";
}

/** Indexes of the points whose value differs from the point before. */
export function changedSegments(points: Point[]): number[] {
  const out: number[] = [];
  for (let i = 1; i < points.length; i++) if (points[i].value !== points[i - 1].value) out.push(i);
  return out;
}

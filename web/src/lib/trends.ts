// Pure helpers of the Overview trends: the latest value, a stacked pair of
// series, and the band layers of a band metric.
import type { Point, Unit } from "../api/model";
import type { TrendSeries } from "../api/overview";
import { fmtValue } from "./units";

export interface EdgePoint {
  time: number;
  low: number;
  high: number;
}

/** The newest point's value; undefined without points. */
export function lastValue(pts: Point[] | undefined): number | undefined {
  return pts && pts.length ? pts[pts.length - 1].value : undefined;
}

/**
 * Two series stacked: the lower one from zero, the upper one on top of it.
 * Only times where both have a point are stacked, so a bucket one of them
 * missed is a gap, never a dip.
 */
export function stack(lower: Point[], upper: Point[]): { lower: EdgePoint[]; upper: EdgePoint[] } {
  const up = new Map(upper.map((p) => [p.time, p.value]));
  const both = lower.filter((p) => up.has(p.time));
  return {
    lower: both.map((p) => ({ time: p.time, low: 0, high: p.value })),
    upper: both.map((p) => ({ time: p.time, low: p.value, high: p.value + up.get(p.time)! })),
  };
}

/** The band of a band metric: points that carry both edges. */
export function bandEdges(s: TrendSeries | undefined): EdgePoint[] {
  return (s?.points ?? []).filter((p) => p.low !== undefined && p.high !== undefined).map((p) => ({ time: p.time, low: p.low!, high: p.high! }));
}

/** "min 14 · max 20" from the series' own min and max; empty without them. */
export function minMaxText(s: { min?: number; max?: number } | undefined, unit: Unit): string {
  if (s?.min === undefined || s.max === undefined) return "";
  if (s.min === s.max) return `steady at ${fmtValue(s.min, unit)}`;
  return `min ${fmtValue(s.min, unit)} · max ${fmtValue(s.max, unit)}`;
}

/** Sum of the points' values; undefined without points. */
export function total(pts: Point[] | undefined): number | undefined {
  return pts && pts.length ? pts.reduce((a, p) => a + p.value, 0) : undefined;
}

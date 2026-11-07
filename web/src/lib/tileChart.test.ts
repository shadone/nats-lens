import { describe, expect, it } from "vitest";
import type { HistoryGap } from "../api/history";
import type { Point } from "../api/model";
import { changedSegments, hiddenMoves, tickedMoves, tileChartReason, type TileChartFacts } from "./tileChart";

const STEP = 900_000;
const START = Date.parse("2026-10-16T14:30:00Z");
const END = START + 96 * STEP;

/** One point per bucket from the range's start. */
const pts = (vals: number[], from = START): Point[] => vals.map((value, i) => ({ time: from + i * STEP, value }));
const full = (v: number) => pts(Array.from({ length: 96 }, () => v));

function facts(extra: Partial<TileChartFacts>): TileChartFacts {
  return { points: full(3), stepMs: STEP, start: START, end: END, status: "ok", gaps: [], ...extra };
}

const gap = (kind: HistoryGap["kind"], start: number, end?: number): HistoryGap => ({ kind, start, end, eventId: "", connection: "" });

describe("tileChartReason", () => {
  it("calls a series with one value over every bucket steady", () => {
    expect(tileChartReason(facts({}))).toBe("steady");
  });

  it("draws a chart when any value differs", () => {
    const p = full(3);
    p[40] = { ...p[40], value: 2 };
    expect(tileChartReason(facts({ points: p }))).toBe("changed");
  });

  it("draws a chart when the second series changes", () => {
    const p2 = full(5);
    p2[95] = { ...p2[95], value: 6 };
    expect(tileChartReason(facts({ points2: p2 }))).toBe("changed");
  });

  it("reads a changes series as changed only when a bucket holds a change", () => {
    expect(tileChartReason(facts({ points: full(0), changesSeries: true }))).toBe("steady");
    const p = full(0);
    p[70] = { ...p[70], value: 2 };
    expect(tileChartReason(facts({ points: p, changesSeries: true }))).toBe("changed");
  });

  it("keeps the chart of a warning or critical tile even when the value is steady", () => {
    expect(tileChartReason(facts({ status: "critical" }))).toBe("trouble");
    expect(tileChartReason(facts({ status: "warning" }))).toBe("trouble");
    expect(tileChartReason(facts({ status: "info" }))).toBe("steady");
    expect(tileChartReason(facts({ status: "muted" }))).toBe("steady");
  });

  it("never calls a tile without points steady", () => {
    expect(tileChartReason(facts({ points: [] }))).toBe("noData");
  });

  it("keeps the chart when a bucket inside the range is missing", () => {
    const p = full(3).filter((_, i) => i !== 50);
    expect(tileChartReason(facts({ points: p }))).toBe("gap");
  });

  it("keeps the chart when the series stops before the range ends", () => {
    // Not collecting for the last three hours.
    expect(tileChartReason(facts({ points: full(3).slice(0, 84) }))).toBe("gap");
  });

  it("tolerates a newest bucket without a point only in a live range", () => {
    expect(tileChartReason(facts({ points: full(3).slice(0, 95), live: true }))).toBe("steady");
    expect(tileChartReason(facts({ points: full(3).slice(0, 95) }))).toBe("gap");
    expect(tileChartReason(facts({ points: full(3).slice(0, 94), live: true }))).toBe("gap");
  });

  it("keeps the chart when the bucket holding the range's start is missing", () => {
    // The range starts five minutes into a bucket; that bucket has no point.
    expect(tileChartReason(facts({ points: full(3).slice(1), start: START + 300_000 }))).toBe("gap");
    expect(tileChartReason(facts({ start: START + 300_000 }))).toBe("steady");
  });

  it("checks the second series for missing buckets too", () => {
    expect(tileChartReason(facts({ points2: full(5).filter((_, i) => i !== 30) }))).toBe("gap");
  });

  it("calls a bucket whose lowest or highest differs from its value changed", () => {
    const p = full(3);
    p[40] = { ...p[40], low: 2, high: 3 };
    expect(tileChartReason(facts({ points: p }))).toBe("changed");
    const q = full(3).map((x) => ({ ...x, low: 3, high: 3 }));
    expect(tileChartReason(facts({ points: q }))).toBe("steady");
  });

  it("calls a series whose min and max differ changed, as the newest report can widen them", () => {
    expect(tileChartReason(facts({ min: 3, max: 4 }))).toBe("changed");
    expect(tileChartReason(facts({ min: 3, max: 3 }))).toBe("steady");
  });

  it("keeps the chart when the series begins after the range and the history start is unknown", () => {
    expect(tileChartReason(facts({ points: pts(Array.from({ length: 40 }, () => 3), START + 56 * STEP) }))).toBe("gap");
  });

  it("calls a series that begins with the history steady, as the track says since when", () => {
    const from = START + 56 * STEP;
    expect(tileChartReason(facts({ points: pts(Array.from({ length: 40 }, () => 3), from), historyStart: from }))).toBe("steady");
    // History began earlier than the series: buckets are missing.
    expect(tileChartReason(facts({ points: pts(Array.from({ length: 40 }, () => 3), from), historyStart: START }))).toBe("gap");
  });

  it("accepts a first bucket that begins before the range", () => {
    // Buckets of a seven-day range start on the step's grid, before the range.
    const step = 6_300_000;
    const end = START + 7 * 86_400_000;
    const first = Math.floor(START / step) * step;
    const p = Array.from({ length: 97 }, (_, i) => ({ time: first + i * step, value: 3 }));
    expect(tileChartReason({ points: p, stepMs: step, start: START, end, status: "ok", gaps: [] })).toBe("steady");
  });

  it("calls a single bucket that covers the range steady", () => {
    expect(tileChartReason(facts({ points: pts([3]), end: START + STEP }))).toBe("steady");
  });

  it("keeps the chart when a collection gap overlaps the range, even inside one bucket", () => {
    expect(tileChartReason(facts({ gaps: [gap("notRunning", START + 10 * STEP, START + 10 * STEP + 180_000)] }))).toBe("gap");
    expect(tileChartReason(facts({ gaps: [gap("notConnected", END - STEP)] }))).toBe("gap");
    expect(tileChartReason(facts({ gaps: [gap("noCollections", START - STEP, START + STEP)] }))).toBe("gap");
  });

  it("ignores gaps outside the range and kinds the charts do not hatch", () => {
    expect(tileChartReason(facts({ gaps: [gap("notRunning", START - 2 * STEP, START - STEP)] }))).toBe("steady");
    expect(tileChartReason(facts({ gaps: [gap("eventsNotCaptured", START + STEP, START + 2 * STEP)] }))).toBe("steady");
    expect(tileChartReason(facts({ gaps: [gap("beforeHistory", START - STEP, START)] }))).toBe("steady");
  });

  it("counts a gap of an unknown kind", () => {
    expect(tileChartReason(facts({ gaps: [gap("unknown", START + STEP, START + 2 * STEP)] }))).toBe("gap");
  });

  it("keeps the chart while the collection gaps are not known", () => {
    expect(tileChartReason(facts({ gaps: undefined }))).toBe("gapsUnknown");
  });
});

describe("changedSegments", () => {
  it("lists the points whose value differs from the one before", () => {
    expect(changedSegments(pts([3, 3, 2, 3, 3]))).toEqual([2, 3]);
    expect(changedSegments(pts([3, 3, 3]))).toEqual([]);
    expect(changedSegments(pts([3]))).toEqual([]);
    expect(changedSegments([])).toEqual([]);
  });
});

describe("hiddenMoves", () => {
  const at = (value: number, low?: number, high?: number, i = 0): Point => ({ time: START + i * STEP, value, low, high });

  it("keeps a bucket that dipped and came back between flat neighbours", () => {
    const p = [at(3, 3, 3, 0), at(3, 2, 3, 1), at(3, 3, 3, 2)];
    expect(hiddenMoves(p)).toEqual([p[1]]);
  });

  it("leaves out a move the line already draws", () => {
    // A dip to 2 seen as a bucket value, its neighbours touching 2 too: the line shows it.
    const p = [at(3, 3, 3, 0), at(3, 2, 3, 1), at(2, 2, 3, 2), at(3, 2, 3, 3), at(3, 3, 3, 4)];
    expect(hiddenMoves(p)).toEqual([]);
  });

  it("leaves out noise that stays between its neighbours' values", () => {
    const p = [at(100, 100, 100, 0), at(105, 101, 109, 1), at(110, 110, 110, 2)];
    expect(hiddenMoves(p)).toEqual([]);
  });

  it("keeps a peak above both neighbours and the bucket's own value", () => {
    const p = [at(100, 100, 100, 0), at(101, 100, 140, 1), at(102, 102, 102, 2)];
    expect(hiddenMoves(p)).toEqual([p[1]]);
  });

  it("compares an end bucket with its own value where a neighbour is missing", () => {
    expect(hiddenMoves([at(3, 2, 3, 0), at(3, 3, 3, 1)])).toHaveLength(1);
    expect(hiddenMoves([at(3, 3, 3, 0), at(3, 3, 4, 1)])).toHaveLength(1);
    expect(hiddenMoves([at(3)])).toEqual([]);
  });
});

describe("tickedMoves", () => {
  const at = (value: number, low: number, high: number, i: number): Point => ({ time: START + i * STEP, value, low, high });

  it("ticks the hidden moves of a series where few buckets hide one", () => {
    const p = [at(3, 3, 3, 0), at(3, 2, 3, 1), at(3, 3, 3, 2), at(3, 3, 3, 3)];
    expect(tickedMoves(p)).toEqual([p[1]]);
  });

  it("ticks nothing when most buckets hide a move, as busy connection counts do", () => {
    const p = Array.from({ length: 10 }, (_, i) => at(100, 90, 110, i));
    expect(hiddenMoves(p)).toHaveLength(10);
    expect(tickedMoves(p)).toEqual([]);
  });
});

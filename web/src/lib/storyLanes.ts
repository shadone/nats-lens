// The findings lane of the Overview's story: the worst state of any check
// per bucket, as runs of time.
import type { Level, TimelineRow } from "../api/model";
import { cellRuns, type CellRun } from "./timelineModel";
import { LEVEL_ORDER } from "./view";

/**
 * The worst state per bucket across the checks. A check that did not run
 * in a bucket does not count; a bucket where none ran is no data.
 */
export function worstPerBucket(rows: TimelineRow[], buckets: number): Level[] {
  return Array.from({ length: buckets }, (_, i) => {
    const ran = rows.map((r) => r.cells[i]).filter((c): c is Level => c !== undefined && c !== "nodata");
    if (!ran.length) return "nodata";
    return ran.reduce((a, b) => (LEVEL_ORDER[b] < LEVEL_ORDER[a] ? b : a));
  });
}

/** Runs of buckets where some check was not ok, by the worst state. */
export function findingRuns(rows: TimelineRow[], start: number, bucketMs: number, buckets: number): CellRun[] {
  return cellRuns(worstPerBucket(rows, buckets), start, bucketMs);
}

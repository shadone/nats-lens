import { describe, expect, it } from "vitest";
import type { TimelineRow } from "../api/model";
import { findingRuns, worstPerBucket } from "./storyLanes";

function row(key: string, cells: TimelineRow["cells"]): TimelineRow {
  return { key, checkCode: key, checkName: key, severity: "warning", cells, now: cells[cells.length - 1] };
}

describe("worstPerBucket", () => {
  it("takes the worst state of every check per bucket, a check that did not run aside", () => {
    const rows = [row("a", ["ok", "warning", "ok", "nodata"]), row("b", ["nodata", "info", "critical", "nodata"])];
    expect(worstPerBucket(rows, 4)).toEqual(["ok", "warning", "critical", "nodata"]);
  });

  it("is no data everywhere without rows", () => {
    expect(worstPerBucket([], 2)).toEqual(["nodata", "nodata"]);
  });
});

describe("findingRuns", () => {
  it("joins neighbouring buckets of one state and leaves ok out", () => {
    const rows = [row("a", ["ok", "warning", "warning", "ok", "critical"])];
    expect(findingRuns(rows, 0, 10, 5)).toEqual([
      { level: "warning", from: 10, to: 30 },
      { level: "critical", from: 40, to: 50 },
    ]);
  });
});

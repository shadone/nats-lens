import { describe, expect, it } from "vitest";
import { bandEdges, inOutText, minMaxText, stack, total } from "./trends";

describe("stack", () => {
  it("puts the upper series on top of the lower one, only where both have a point", () => {
    const r3 = [{ time: 0, value: 36 }, { time: 10, value: 36 }, { time: 20, value: 36 }];
    const r1 = [{ time: 0, value: 23 }, { time: 20, value: 101 }];
    const s = stack(r3, r1);
    expect(s.lower).toEqual([{ time: 0, low: 0, high: 36 }, { time: 20, low: 0, high: 36 }]);
    expect(s.upper).toEqual([{ time: 0, low: 36, high: 59 }, { time: 20, low: 36, high: 137 }]);
  });
});

describe("bandEdges", () => {
  it("keeps only points with both edges", () => {
    const s = { metric: "cluster.cpu_percent", unit: "percent" as const, stepMs: 1, topPoints: [], points: [{ time: 0, value: 5, low: 2, high: 40 }, { time: 1, value: 4 }] };
    expect(bandEdges(s)).toEqual([{ time: 0, low: 2, high: 40 }]);
  });
});

describe("minMaxText", () => {
  it("says steady when min equals max, and nothing without them", () => {
    expect(minMaxText({ min: 3, max: 3 }, "count")).toBe("steady at 3");
    expect(minMaxText({ min: 14, max: 20 }, "count")).toBe("min 14 · max 20");
    expect(minMaxText(undefined, "count")).toBe("");
  });
});

describe("total", () => {
  it("is undefined without points, never 0", () => {
    expect(total([])).toBeUndefined();
    expect(total([{ time: 0, value: 0 }, { time: 1, value: 2 }])).toBe(2);
  });
});

describe("inOutText", () => {
  it("writes the unit once, so the header stays on one line", () => {
    expect(inOutText(1.45 * 1048576, 4.59 * 1048576, "bytes_per_second")).toBe("1.45 / 4.59 MiB/s");
    expect(inOutText(1240, 3910, "per_second")).toBe("1,240 / 3,910/s");
  });

  it("keeps each unit when the two differ, and a dash for a value not observed", () => {
    expect(inOutText(900, 2 * 1048576, "bytes_per_second")).toBe("900 B/s / 2 MiB/s");
    expect(inOutText(undefined, 3910, "per_second")).toBe("– / 3,910/s");
    expect(inOutText(undefined, undefined, "per_second")).toBeUndefined();
  });
});

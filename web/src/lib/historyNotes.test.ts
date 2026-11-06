import { describe, expect, it } from "vitest";
import { growthHistoryNote, historyNote, rangeCoverageText } from "./historyView";
const H = 3_600_000;
const end = Date.parse("2026-10-17T14:30:00Z");
describe("history words on range pages", () => {
  it("explains a young history without shortening the requested range", () => {
    expect(historyNote(end - 31 * 60_000, end - 24 * H, end, 30_000, true)).toBe("31 min of history: you asked for the last 24 h; nats-lens started at 13:59 UTC.");
    expect(rangeCoverageText(end - 31 * 60_000, end - 24 * H, end)).toBe("over 31 min");
  });
  it("says nothing when history covers the range or differs by at most an interval", () => {
    expect(historyNote(end - 24 * H + 30_000, end - 24 * H, end, 30_000, true)).toBeUndefined();
    expect(historyNote(undefined, end - H, end, 30_000, true)).toBeUndefined();
    expect(rangeCoverageText(end - 25 * H, end - 24 * H, end)).toBeUndefined();
  });
  it("never predicts entity readiness from retained age", () => {
    expect(growthHistoryNote(end - 31 * 60_000, end - 24 * H, end, true)).toBe("needs samples spanning 6 h; entity data may be missing");
    expect(growthHistoryNote(end - 31 * 60_000, end - H, end, true)).toBe("needs 6 h of history; choose a range of at least 6 h");
    expect(growthHistoryNote(end - H, end - 24 * H, end, false)).toBe("needs samples spanning 6 h; entity data may be missing");
    expect(growthHistoryNote(end - 8 * H, end - 24 * H, end, true)).toBe("needs samples spanning 6 h; entity data may be missing");
  });
});

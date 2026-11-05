import { describe, expect, it } from "vitest";
import { gap, NOW, staging } from "../test/fixtures/history";
import { askedText, atHistoryStart, atStartText, gapMarks, growthWaitText, historyInRange, historyNoteText, tileSpanText, type HistoryFrame } from "./historyNotes";
import { isHatched } from "../api/history";

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const LIVE_24H = { kind: "live", preset: "24h" } as const;

function frame(over: Partial<HistoryFrame> = {}): HistoryFrame {
  return { historyStart: NOW - 31 * MIN, start: NOW - DAY, end: NOW, live: true, now: NOW, ...over };
}

describe("the history note", () => {
  it("says how much history the range holds and where it begins", () => {
    expect(historyNoteText(frame(), LIVE_24H, 30_000)).toBe("31 min of history: you asked for the last 24 h; the history begins at 22:16 UTC.");
  });

  it("is absent when the history covers the range, or misses it by no more than an interval", () => {
    expect(historyNoteText(frame({ historyStart: NOW - 2 * DAY }), LIVE_24H, 30_000)).toBeUndefined();
    expect(historyNoteText(frame({ historyStart: NOW - DAY + 30_000 }), LIVE_24H, 30_000)).toBeUndefined();
    expect(historyNoteText(frame({ historyStart: NOW - DAY + 31_000 }), LIVE_24H, 30_000)).toBeDefined();
  });

  it("is absent without a history", () => {
    expect(historyNoteText(frame({ historyStart: undefined }), LIVE_24H, 30_000)).toBeUndefined();
  });

  it("names a fixed range by its ends, and says when the range holds no history", () => {
    const start = Date.parse("2026-11-04T00:00:00Z");
    const end = Date.parse("2026-11-05T00:00:00Z");
    const spec = { kind: "fixed", from: start, to: end } as const;
    expect(historyNoteText(frame({ start, end, live: false }), spec, 30_000)).toBe(
      "No history in this range: you asked for 4 Nov 00:00 to 5 Nov 00:00 UTC; the history begins at 22:16 UTC.",
    );
  });

  it("words each preset", () => {
    expect(askedText({ kind: "live", preset: "1h" }, 0, 0)).toBe("the last hour");
    expect(askedText({ kind: "live", preset: "7d" }, 0, 0)).toBe("the last 7 days");
  });
});

describe("the history in a range", () => {
  it("runs from the later of the range start and the history start", () => {
    expect(historyInRange(frame())).toBe(31 * MIN);
    expect(historyInRange(frame({ historyStart: NOW - 2 * DAY }))).toBe(DAY);
    expect(historyInRange(frame({ historyStart: NOW + MIN }))).toBe(0);
    expect(historyInRange(frame({ historyStart: undefined }))).toBeUndefined();
  });
});

describe("how long a growth rate still needs", () => {
  it("says the time to go on a range that follows now", () => {
    expect(growthWaitText(frame())).toBe("needs 6 h of history; 5 h 29 min to go");
  });

  it("says what an ended range holds", () => {
    expect(growthWaitText(frame({ live: false, end: NOW - HOUR, start: NOW - HOUR - DAY, historyStart: NOW - 2 * HOUR }))).toBe("needs 6 h of history; this range holds 1 h");
  });

  it("says when the range itself is too short", () => {
    expect(growthWaitText(frame({ start: NOW - HOUR }))).toBe("needs 6 h of history; this range is 1 h");
  });

  it("says what the range has, and since when nothing is collected, while the collector has stopped", () => {
    const stopped = frame({ historyStart: NOW - 9 * HOUR, stoppedAt: NOW - 3 * HOUR, lastDataAt: NOW - 3 * HOUR - 4_000 });
    expect(growthWaitText(stopped)).toBe("needs 6 h of history; has 5 h 59 min, not collecting since 19:47 UTC");
    // A stop after the range's end changes nothing.
    expect(growthWaitText({ ...stopped, end: NOW - 4 * HOUR, start: NOW - DAY })).toBe("needs 6 h of history; 1 h to go");
  });

  it("says nothing once the range holds 6 hours, or without a history", () => {
    expect(growthWaitText(frame({ historyStart: NOW - 6 * HOUR }))).toBeUndefined();
    expect(growthWaitText(frame({ historyStart: undefined }))).toBeUndefined();
  });
});

describe("a tile's span", () => {
  it("says over how long when the history is shorter than the range", () => {
    expect(tileSpanText(frame(), 30_000)).toBe("over 31 min");
    expect(tileSpanText(frame({ historyStart: NOW + MIN }), 30_000)).toBe("no history in this range");
  });

  it("says nothing when the history covers the range", () => {
    expect(tileSpanText(frame({ historyStart: NOW - 2 * DAY }), 30_000)).toBeUndefined();
    expect(tileSpanText(frame({ historyStart: undefined }), 30_000)).toBeUndefined();
  });
});

describe("findings already true when the history begins", () => {
  const h = NOW - 9 * HOUR;
  const f = { since: h + 20_000, state: "firing" as const };

  it("follows the finding page's rule", () => {
    expect(atHistoryStart(f, h)).toBe(true);
    expect(atHistoryStart({ ...f, since: h + HOUR, conditionSince: h - 1 }, h)).toBe(true);
    expect(atHistoryStart({ ...f, since: h + HOUR }, h)).toBe(false);
    expect(atHistoryStart({ ...f, state: "resolved" }, h)).toBe(false);
    expect(atHistoryStart(f, undefined)).toBe(false);
  });

  it("words one finding and a group", () => {
    expect(atStartText(1, 1, h, NOW)).toBe("Already true when the history begins, at 13:47 UTC.");
    expect(atStartText(3, 3, h, NOW)).toBe("All 3 were already true when the history begins, at 13:47 UTC.");
    expect(atStartText(2, 4, h, NOW)).toBe("2 of 4 were already true when the history begins, at 13:47 UTC.");
  });
});

describe("gaps on a chart", () => {
  const d = staging();
  const g = { gaps: d.gaps.filter(isHatched), rangeEnd: d.rangeEnd, live: true };

  it("places the hatched kinds only, oldest left, each named", () => {
    const marks = gapMarks(g, d.rangeStart, d.rangeEnd, NOW);
    expect(marks.map((m) => m.name)).toEqual([
      "No data 4 Nov 22:47:18–5 Nov 13:47:18 UTC: nats-lens was not running yet",
      "No data 17:50:44–17:53:44 UTC: nats-lens was not running",
      "No data from 19:47:14 UTC until now: nats-lens could not reach NATS",
    ]);
    expect(marks[0]).toMatchObject({ left: 0, width: 62.5 });
    expect(marks[1].width).toBeCloseTo(0.21, 2);
    // An open gap runs to the chart's end.
    expect(marks[2].left + marks[2].width).toBeCloseTo(100, 1);
  });

  it("cuts a gap to the chart's own span, and leaves out one outside it", () => {
    const marks = gapMarks(g, NOW - 6 * HOUR, NOW - 4 * HOUR, NOW);
    expect(marks.map((m) => m.name)).toEqual(["No data 17:50:44–17:53:44 UTC: nats-lens was not running"]);
  });

  it("draws nothing without gaps or before the answer", () => {
    expect(gapMarks(undefined, 0, 1, NOW)).toEqual([]);
    expect(gapMarks({ ...g, gaps: [] }, 0, 1, NOW)).toEqual([]);
    expect(gapMarks({ ...g, gaps: [gap({ kind: "eventsNotCaptured", start: NOW - HOUR, end: NOW })] }, NOW - DAY, NOW, NOW)).toEqual([]);
  });
});

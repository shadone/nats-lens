import { describe, expect, it } from "vitest";
import { gap, NOW, prod, staging, use } from "../test/fixtures/history";
import {
  archiveHoursText,
  coveredShare,
  enoughState,
  findingsAtStartText,
  gapCountLines,
  gapDetail,
  gapEndText,
  gapLabel,
  gapLengthText,
  gapName,
  historyUseLabel,
  historyUseLine,
  lastErrorText,
  lastExportText,
  retentionLine,
  shareText,
  spanText,
  stripEnds,
  stripSegments,
  summaryParts,
  whenText,
} from "./historyView";

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

describe("spanText", () => {
  it("gives the two largest units", () => {
    expect(spanText(45_000)).toBe("45 s");
    expect(spanText(31 * MIN)).toBe("31 min");
    expect(spanText(4 * MIN + 10_000)).toBe("4 min 10 s");
    expect(spanText(5 * HOUR + 29 * MIN)).toBe("5 h 29 min");
    expect(spanText(6 * HOUR)).toBe("6 h");
    expect(spanText(13 * DAY + 6 * HOUR + 5 * MIN)).toBe("13 d 6 h");
    expect(spanText(30 * DAY)).toBe("30 d");
  });

  it("says under a second, never 0 s, for a span above 0", () => {
    expect(spanText(400)).toBe("<1 s");
    expect(spanText(0)).toBe("0 s");
  });

  it("writes days and hours out in the long form", () => {
    expect(spanText(13 * DAY + 6 * HOUR, true)).toBe("13 days 6 hours");
    expect(spanText(DAY + HOUR, true)).toBe("1 day 1 hour");
    expect(spanText(14 * DAY, true)).toBe("14 days");
    expect(spanText(31 * MIN, true)).toBe("31 min");
  });
});

describe("whenText", () => {
  it("gives the time alone on now's day, the date otherwise", () => {
    expect(whenText(Date.parse("2026-11-05T13:47:18Z"), NOW)).toBe("13:47 UTC");
    expect(whenText(Date.parse("2026-11-06T13:47:18Z"), NOW)).toBe("2026-11-06 13:47 UTC");
    expect(whenText(Date.parse("2026-10-22T09:00:00Z"), NOW)).toBe("2026-10-22 09:00 UTC");
  });
});

describe("summaryParts", () => {
  it("says since when, how long it is kept and archived", () => {
    expect(summaryParts(prod(), NOW)).toEqual(["Collecting since 2026-10-06 22:47 UTC (30 days)", "kept for 30 days", "archived for 90 days", "events from 2026-11-04 22:47 UTC"]);
  });

  it("says the stop, no archive, and the oldest event when it is not the history start", () => {
    expect(summaryParts(staging(), NOW, NOW - 3 * HOUR)).toEqual(["History since 13:47 UTC (9 hours)", "not collecting since 19:47 UTC", "kept for 14 days", "no archive", "events from 13:50 UTC"]);
  });

  it("leaves the oldest event out within an interval of the history start", () => {
    const d = { ...staging(), eventsStart: staging().historyStart! + 20_000 };
    expect(summaryParts(d, NOW).some((p) => p.startsWith("events from"))).toBe(false);
  });

  it("says kept forever and every hour kept without retentions", () => {
    const d = { ...prod(), retention: undefined, archive: { ...prod().archive, retention: undefined } };
    expect(summaryParts(d, NOW)).toEqual(["Collecting since 2026-10-06 22:47 UTC (30 days)", "kept forever", "archived, every hour kept", "events from 2026-11-04 22:47 UTC"]);
  });

  it("says no collection is stored without a history", () => {
    expect(summaryParts({ ...staging(), historyStart: undefined }, NOW)).toEqual(["No collection stored yet"]);
  });
});

describe("gaps", () => {
  it("labels each kind as the plan words it", () => {
    expect(["notRunning", "notConnected", "noCollections", "eventsNotCaptured", "kubernetesNotWatched", "beforeHistory"].map((k) => gapLabel(k as never))).toEqual([
      "nats-lens was not running",
      "nats-lens could not reach NATS",
      "No collections",
      "Events not captured",
      "Kubernetes not watched",
      "nats-lens was not running yet",
    ]);
  });

  it("says that collections went on only for the kinds charts do not hatch", () => {
    expect(gapDetail(gap({ kind: "eventsNotCaptured", connection: "sys" }))).toBe("connection sys; collections went on");
    expect(gapDetail(gap({ kind: "kubernetesNotWatched" }))).toBe("collections went on");
    expect(gapDetail(gap({ kind: "notRunning" }))).toBeUndefined();
  });

  it("ends an open gap still now on a live range and after the range on a fixed one", () => {
    const open = gap({ kind: "notConnected", start: NOW - 3 * HOUR });
    expect(gapEndText(open, NOW, true, NOW)).toBe("still now");
    expect(gapEndText(open, NOW - HOUR, false, NOW)).toBe("after the range");
    expect(gapLengthText(open, NOW, true, NOW)).toBe("3 h so far");
    expect(gapLengthText(open, NOW - HOUR, false, NOW)).toBe("at least 2 h");
    expect(gapName(open, NOW, true, NOW)).toBe("No data from 19:47:18 UTC until now: nats-lens could not reach NATS");
    expect(gapName(open, NOW - HOUR, false, NOW)).toBe("No data from 19:47:18 UTC until after the range: nats-lens could not reach NATS");
  });

  it("reads a fixed range that ends at or after now as one that follows now", () => {
    const open = gap({ kind: "notConnected", start: NOW - 3 * HOUR });
    expect(gapEndText(open, NOW, false, NOW)).toBe("still now");
    expect(gapEndText(open, NOW + HOUR, false, NOW)).toBe("still now");
    expect(gapLengthText(open, NOW, false, NOW)).toBe("3 h so far");
    expect(gapName(open, NOW, false, NOW)).toMatch(/until now:/);
  });

  it("gives a closed gap its end and length, with the date a day before the range end", () => {
    const g = gap({ kind: "notRunning", start: Date.parse("2026-11-05T17:50:44Z"), end: Date.parse("2026-11-05T17:53:44Z") });
    expect(gapEndText(g, NOW, true, NOW)).toBe("17:53:44");
    expect(gapLengthText(g, NOW, true, NOW)).toBe("3 min");
    expect(gapName(g, NOW, true, NOW)).toBe("No data 17:50:44–17:53:44 UTC: nats-lens was not running");
    expect(gapEndText(gap({ end: Date.parse("2026-11-01T10:32:51Z") }), NOW, false, NOW)).toBe("1 Nov 10:32");
  });

  it("names both ends of a stretch in one format, with seconds and the day when either needs it", () => {
    const g = gap({ kind: "notRunning", start: Date.parse("2026-10-16T14:30:00Z"), end: Date.parse("2026-11-05T05:30:00Z") });
    expect(gapName(g, NOW, true, NOW)).toBe("No data 16 Oct 14:30:00–5 Nov 05:30:00 UTC: nats-lens was not running");
    const old = gap({ kind: "noCollections", start: Date.parse("2026-11-01T10:27:51Z") });
    expect(gapName(old, NOW, true, NOW)).toBe("No data from 1 Nov 10:27:51 UTC until now: No collections");
  });

  it("counts each class that was cut at 200 apart", () => {
    const hatched = Array.from({ length: 200 }, () => gap());
    const other = Array.from({ length: 3 }, () => gap({ kind: "eventsNotCaptured" }));
    expect(gapCountLines({ gaps: [...hatched, ...other], gapsTotal: 243, listedGapsTotal: 3 })).toEqual(["Showing 200 of 243 times without collections, the newest."]);
    expect(gapCountLines({ gaps: [...other], gapsTotal: 0, listedGapsTotal: 230 })).toEqual(["Showing 3 of 230 times without events or Kubernetes, the newest."]);
    expect(gapCountLines(staging())).toEqual([]);
  });

  it("places the hatched gaps on the range oldest first, an open one to its end, a gap from before the range cut to its start", () => {
    const d = staging();
    const segs = stripSegments(d);
    expect(segs.map((s) => s.gap.kind)).toEqual(["beforeHistory", "notRunning", "notConnected"]);
    expect(segs[0]).toMatchObject({ left: 0, width: 62.5 });
    expect(segs[2].left + segs[2].width).toBe(100);
    const early = stripSegments({ ...d, gaps: [gap({ kind: "notRunning", start: d.rangeStart - HOUR, end: d.rangeStart + 6 * HOUR })] });
    expect(early[0]).toMatchObject({ left: 0, width: 25 });
  });
});

describe("stripEnds", () => {
  it("labels the ends with times under a day, with days from a day, and now on a live range", () => {
    expect(stripEnds({ rangeStart: NOW - 6 * HOUR, rangeEnd: NOW }, false)).toEqual(["16:47", "22:47"]);
    expect(stripEnds({ rangeStart: NOW - 30 * DAY, rangeEnd: NOW }, false)).toEqual(["6 Oct 22:47", "5 Nov 22:47"]);
    expect(stripEnds({ rangeStart: NOW - DAY, rangeEnd: NOW }, true)).toEqual(["4 Nov 22:47", "now"]);
  });
});

describe("coverage", () => {
  it("gives the share with collections, never 100% with a gap or 0% with data", () => {
    expect(shareText(coveredShare(staging().coverage)!, 10_984_000)).toBe("66%");
    expect(shareText(1, 0)).toBe("100%");
    expect(shareText(0.9999, 1000)).toBe("99%");
    expect(shareText(1, 1)).toBe("99%");
    expect(shareText(0.004, 1000)).toBe("<1%");
    expect(shareText(0, 1000, 5)).toBe("<1%");
    expect(shareText(0, 1000, 0)).toBe("0%");
  });

  it("has no share without history in the range", () => {
    expect(coveredShare({ epochs: 0, expected: 0, partial: 0 })).toBeUndefined();
  });
});

describe("uses", () => {
  it("names each use and what reads it", () => {
    expect(historyUseLabel({ id: "growth", checkCode: "" }).title).toBe("Growth per day");
    expect(historyUseLabel({ id: "forecast:server-014", checkCode: "server-014" })).toEqual({ title: "Store forecast", sub: "server-014: when a server's store fills" });
    expect(historyUseLabel({ id: "lookback:consumer-002", checkCode: "consumer-002" }).title).toBe("Check consumer-002");
    expect(historyUseLabel({ id: "accept_review", checkCode: "" }).title).toBe("Growth review of accepted findings");
    expect(historyUseLabel({ id: "day", checkCode: "" }).sub).toBe("Applications traffic, failed logins (Accounts), drops (Topology)");
    expect(historyUseLabel({ id: "replay", checkCode: "" }).title).toBe("Replay");
    expect(historyUseLabel({ id: "later", checkCode: "" }).title).toBe("later");
  });

  it("words enough, not yet and never", () => {
    expect(historyUseLine(use(), 14 * DAY, NOW)).toBe("needs 6 h; has 9 h; enough");
    const notYet = use({ has: 31 * MIN, enough: false, enoughAt: Date.parse("2026-11-05T15:02:00Z") });
    expect(historyUseLine(notYet, 14 * DAY, Date.parse("2026-11-05T09:33:00Z"))).toBe("needs 6 h; has 31 min; enough at 15:02 UTC");
    const never = use({ id: "accept_review", needs: 8 * DAY + HOUR, has: 2 * DAY, enough: false, never: true });
    expect(enoughState(never, 7 * DAY, NOW)).toEqual({ text: "never with retention 7 d", tone: "warn" });
    expect(enoughState(use({ enough: false }), 7 * DAY, NOW)).toEqual({ text: "not within this range", tone: "muted" });
  });

  it("says enough once the time it named has passed", () => {
    expect(enoughState(use({ enough: false, enoughAt: NOW - MIN }), 7 * DAY, NOW)).toEqual({ text: "enough", tone: "ok" });
    expect(enoughState(use({ enough: false, enoughAt: NOW }), 7 * DAY, NOW)).toEqual({ text: "enough", tone: "ok" });
  });

  it("says has none without history", () => {
    expect(historyUseLine(use({ has: undefined, enough: false }), 7 * DAY, NOW)).toBe("needs 6 h; has none; not within this range");
  });
});

describe("retention and archive", () => {
  it("says how long the database keeps data", () => {
    expect(retentionLine(staging(), NOW)).toBe("Collections and events are kept for 14 days: the hourly sweep removes what is older than 2026-10-22 22:47 UTC.");
    expect(retentionLine({ retention: undefined, retentionFrom: undefined }, NOW)).toBe("Collections and events are kept forever: no retention is set.");
  });

  it("gives the archived hours, or says they are not listed yet", () => {
    const a = prod().archive;
    expect(archiveHoursText(a)).toBe("2,161, from 2026-08-07 22:00 to 2026-11-05 22:00 UTC");
    expect(archiveHoursText({ ...a, listedAt: undefined, hours: undefined })).toMatch(/^not listed yet/);
    expect(archiveHoursText({ ...a, hours: 0, oldestHour: undefined, newestHour: undefined })).toBe("none yet");
  });

  it("says whether the newest hour was exported provisionally", () => {
    const a = prod().archive;
    expect(lastExportText(a, NOW)).toBe("22:44 UTC, the open hour provisionally");
    expect(lastExportText({ ...a, newestFinal: true }, NOW)).toBe("22:44 UTC, the newest hour after it closed");
    expect(lastExportText({ ...a, lastExportAt: undefined }, NOW)).toBe("none yet");
  });

  it("gives the last error with its time", () => {
    expect(lastErrorText(prod().archive, NOW)).toBeUndefined();
    expect(lastErrorText({ ...prod().archive, lastError: "disk full", lastErrorAt: Date.parse("2026-11-05T21:00:00Z") }, NOW)).toBe("Last export failed at 21:00 UTC: disk full");
  });
});

describe("findingsAtStartText", () => {
  it("counts the findings already true at the history start", () => {
    expect(findingsAtStartText(4, NOW - 9 * HOUR, NOW)).toBe("4 findings were already true when the history begins, at 13:47 UTC.");
    expect(findingsAtStartText(1, NOW - 9 * HOUR, NOW)).toBe("1 finding was already true when the history begins, at 13:47 UTC.");
    expect(findingsAtStartText(0, NOW - 9 * HOUR, NOW)).toBe("No finding was already true when the history begins, at 13:47 UTC.");
  });
});

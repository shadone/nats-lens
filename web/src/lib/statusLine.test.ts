import { describe, expect, it } from "vitest";
import type { Freshness, Tally } from "../api/model";
import { statusLine } from "./statusLine";

const NOW = Date.parse("2026-10-17T14:30:00Z");
const fresh: Freshness = { epoch: "9", epochAt: NOW - 10_000, ageMs: 10_000, partial: false, live: true, intervalMs: 30_000 };
const none: Tally = { critical: 0, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0 };

describe("statusLine", () => {
  it("serves normally with nothing or only info firing", () => {
    expect(statusLine(none, fresh, true, NOW)).toEqual({ level: "ok", text: "Serving normally", badges: [] });
    expect(statusLine({ ...none, info: 1, muted: 3 }, fresh, true, NOW)).toEqual({ level: "ok", text: "Serving normally", badges: [{ level: "info", count: 1 }] });
  });

  it("follows the worst unmuted severity and counts each severity", () => {
    const s = statusLine({ ...none, critical: 1, warning: 2, info: 1 }, fresh, true, NOW);
    expect(s.level).toBe("critical");
    expect(s.text).toBe("Critical findings");
    expect(s.badges).toEqual([
      { level: "critical", count: 1 },
      { level: "warning", count: 2 },
      { level: "info", count: 1 },
    ]);
    expect(statusLine({ ...none, warning: 2 }, fresh, true, NOW).text).toBe("Needs attention");
  });

  it("says not collecting when the newest epoch is older than three intervals in a live range", () => {
    const old = { ...fresh, epochAt: NOW - 120_000, ageMs: 120_000 };
    expect(statusLine({ ...none, warning: 1 }, old, true, NOW)).toMatchObject({ level: "nodata", text: "Not collecting" });
    expect(statusLine(none, { ...fresh, live: false }, true, NOW).text).toBe("Not collecting");
  });

  it("judges a fixed range by its findings only", () => {
    const old = { ...fresh, epochAt: NOW - 86_400_000, ageMs: 86_400_000, live: false };
    expect(statusLine(none, old, false, NOW).text).toBe("Serving normally");
  });

  it("has no data without an epoch", () => {
    expect(statusLine(none, undefined, true, NOW)).toEqual({ level: "nodata", text: "No data yet", badges: [] });
  });
});

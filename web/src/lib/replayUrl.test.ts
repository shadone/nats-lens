import { describe, expect, it } from "vitest";
import type { Window } from "../api/model";
import { clampMoments, compactMoment, defaultMoments, momentIso, parseMoment, rangeNotes, resolveMoments, shareParams } from "./replayUrl";

const T = (s: string) => Date.parse(s);
const NOW = T("2026-10-17T12:00:00Z");
const RANGE = { start: NOW - 24 * 3_600_000, end: NOW };
const W = (start: string, end: string): Window => ({ start: T(start), end: T(end), label: "GKE node upgrade", detail: "" });
const ctx = (windows?: Window[]) => ({ windows, range: RANGE, now: NOW, minGapMs: 30_000 });

describe("parseMoment", () => {
  it("reads UTC times with a Z and refuses the rest", () => {
    expect(parseMoment("2026-10-17T09:14:00Z")).toBe(T("2026-10-17T09:14:00Z"));
    expect(parseMoment("2026-10-17T09:14Z")).toBe(T("2026-10-17T09:14:00Z"));
    expect(parseMoment("2026-10-17T09:14:00")).toBeUndefined();
    expect(parseMoment("yesterday")).toBeUndefined();
    expect(parseMoment(null)).toBeUndefined();
  });

  it("refuses a day that does not exist instead of rolling it over", () => {
    expect(parseMoment("2026-02-30T00:00Z")).toBeUndefined();
    expect(parseMoment("2026-10-17T24:30Z")).toBeUndefined();
  });

  it("writes to the second", () => {
    expect(momentIso(T("2026-10-17T09:14:05.678Z"))).toBe("2026-10-17T09:14:05Z");
    expect(compactMoment(T("2026-10-17T09:14:05Z"))).toBe("20261017T091405Z");
  });
});

describe("defaultMoments", () => {
  it("takes the newest Kubernetes window of the range, five minutes wider on each side", () => {
    const m = defaultMoments([W("2026-10-17T02:00:00Z", "2026-10-17T03:30:00Z"), W("2026-10-17T09:14:00Z", "2026-10-17T09:50:00Z")], RANGE, NOW);
    expect(m).toEqual({ a: T("2026-10-17T09:09:00Z"), b: T("2026-10-17T09:55:00Z") });
  });

  it("ends a window that runs up to now at now", () => {
    const m = defaultMoments([W("2026-10-17T11:30:00Z", "2026-10-17T11:58:00Z")], RANGE, NOW);
    expect(m.b).toBe(NOW);
  });

  it("ignores a window outside the range and falls back to the range's last hour", () => {
    const m = defaultMoments([W("2026-10-14T09:00:00Z", "2026-10-14T10:00:00Z")], RANGE, NOW);
    expect(m).toEqual({ a: NOW - 3_600_000, b: NOW });
  });
});

describe("clampMoments", () => {
  it("swaps A after B and says so", () => {
    const c = clampMoments({ a: T("2026-10-17T10:00:00Z"), b: T("2026-10-17T09:00:00Z") }, NOW, 30_000);
    expect([c.a, c.b]).toEqual([T("2026-10-17T09:00:00Z"), T("2026-10-17T10:00:00Z")]);
    expect(c.notes).toEqual(["A was after B, so the two were swapped."]);
  });

  it("moves A to 24 hours before B", () => {
    const c = clampMoments({ a: T("2026-10-15T09:00:00Z"), b: T("2026-10-17T09:00:00Z") }, NOW, 30_000);
    expect(c.a).toBe(T("2026-10-16T09:00:00Z"));
    expect(c.notes[0]).toMatch(/at most 24 hours/);
  });

  it("keeps A at least one interval before B and B no later than now", () => {
    expect(clampMoments({ a: NOW - 10_000, b: NOW }, NOW, 30_000).a).toBe(NOW - 30_000);
    const c = clampMoments({ a: NOW - 3_600_000, b: NOW + 60_000 }, NOW, 30_000);
    expect(c.b).toBe(NOW);
    expect(c.notes).toEqual(["B is after now, so it is read as now."]);
  });
});

describe("clampMoments and the future", () => {
  it("keeps a swapped future B at now", () => {
    const c = clampMoments({ a: NOW + 3_600_000, b: NOW - 3_600_000 }, NOW, 30_000);
    expect([c.a, c.b]).toEqual([NOW - 3_600_000, NOW]);
    expect(c.notes).toEqual(["A was after B, so the two were swapped.", "B is after now, so it is read as now."]);
  });

  it("says an A exactly at now is at or after now", () => {
    expect(clampMoments({ a: NOW, b: NOW }, NOW, 30_000).notes).toEqual(["A is at or after now, so it is read as an hour before now."]);
  });

  it("reads A and B both in the future as the hour before now", () => {
    const c = clampMoments({ a: NOW + 3_600_000, b: NOW + 7_200_000 }, NOW, 30_000);
    expect([c.a, c.b]).toEqual([NOW - 3_600_000, NOW]);
    expect(c.notes).toEqual(["B is after now, so it is read as now.", "A is at or after now, so it is read as an hour before now."]);
  });

  it("moves an A equal to B in the past back by one collect interval, without calling it after now", () => {
    const c = clampMoments({ a: T("2026-10-17T09:00:00Z"), b: T("2026-10-17T09:00:00Z") }, NOW, 30_000);
    expect(c.a).toBe(T("2026-10-17T09:00:00Z") - 30_000);
    expect(c.notes).toEqual(["A must be at least one collect interval before B, so A was moved."]);
  });

  it("reads a lone B in the future as the hour before now, saying nothing of an A the link lacks", () => {
    const r = resolveMoments(null, "2026-10-17T14:00:00Z", ctx(undefined))!;
    expect([r.a, r.b]).toEqual([NOW - 3_600_000, NOW]);
    expect(r.notes).toEqual(["The link's B is after now, so the hour before now is shown."]);
  });

  it("reads a lone A in the future as the hour before now", () => {
    const r = resolveMoments("2099-01-01T00:00:00Z", null, ctx(undefined))!;
    expect([r.a, r.b]).toEqual([NOW - 3_600_000, NOW]);
    expect(r.notes).toEqual(["The link's A is after now, so the hour before now is shown."]);
  });
});

describe("resolveMoments", () => {
  it("waits for the windows when the link has neither moment, then asks to write the default", () => {
    expect(resolveMoments(null, null, ctx(undefined))).toBeUndefined();
    const r = resolveMoments(null, null, ctx([W("2026-10-17T09:14:00Z", "2026-10-17T09:50:00Z")]));
    expect(r).toEqual({ a: T("2026-10-17T09:09:00Z"), b: T("2026-10-17T09:55:00Z"), notes: [], write: true });
  });

  it("reads both moments without writing", () => {
    const r = resolveMoments("2026-10-17T09:14:00Z", "2026-10-17T09:50:00Z", ctx(undefined));
    expect(r).toEqual({ a: T("2026-10-17T09:14:00Z"), b: T("2026-10-17T09:50:00Z"), notes: [], write: false });
  });

  it("gives a lone A an hour, and says when a moment is no valid time", () => {
    const r = resolveMoments("2026-10-17T09:00:00Z", "soon", ctx(undefined))!;
    expect([r.a, r.b, r.write]).toEqual([T("2026-10-17T09:00:00Z"), T("2026-10-17T10:00:00Z"), true]);
    expect(r.notes).toEqual(['The link\'s B "soon" is not a valid UTC time, so a default was used.']);
  });

  it("explains a link longer than 24 hours and keeps its B", () => {
    const r = resolveMoments("2026-10-15T09:00:00Z", "2026-10-17T09:00:00Z", ctx(undefined))!;
    expect(r.a).toBe(T("2026-10-16T09:00:00Z"));
    expect(r.write).toBe(true);
    expect(r.notes).toHaveLength(1);
  });
});

describe("rangeNotes", () => {
  it("names a moment outside the page's range", () => {
    expect(rangeNotes({ a: RANGE.start - 1, b: NOW - 1 }, RANGE)).toEqual(["A is before this range."]);
    expect(rangeNotes({ a: RANGE.start - 2, b: RANGE.start - 1 }, RANGE)).toEqual(["A and B are before this range."]);
    expect(rangeNotes({ a: NOW - 2, b: NOW - 1 }, RANGE)).toEqual([]);
  });
});

describe("shareParams", () => {
  it("fixes a live range at now and keeps the system", () => {
    const p = shareParams(new URLSearchParams("range=6h&system=example-global"), { kind: "live", preset: "6h" }, { a: T("2026-10-17T09:14:00Z"), b: T("2026-10-17T09:50:00Z") }, NOW);
    expect(Object.fromEntries(p)).toEqual({
      system: "example-global",
      from: "2026-10-17T06:00:00Z",
      to: "2026-10-17T12:00:00Z",
      a: "2026-10-17T09:14:00Z",
      b: "2026-10-17T09:50:00Z",
    });
  });
});

import { describe, expect, it } from "vitest";
import {
  bucketFor,
  parseRange,
  rangeProblem,
  pauseRange,
  resolveRange,
  resumeLive,
  writeRange,
} from "./timeRange";

const p = (q: string) => new URLSearchParams(q);
const NOW = Date.parse("2026-10-17T14:30:00Z");

describe("parseRange", () => {
  it("defaults to the last 24 hours, live", () => {
    expect(parseRange(p(""), NOW)).toEqual({ kind: "live", preset: "24h" });
  });

  it("reads a preset", () => {
    expect(parseRange(p("range=6h"), NOW)).toEqual({ kind: "live", preset: "6h" });
  });

  it("ignores an unknown preset", () => {
    expect(parseRange(p("range=3w"), NOW)).toEqual({ kind: "live", preset: "24h" });
  });

  it("reads a fixed range, which wins over a preset", () => {
    expect(parseRange(p("range=1h&from=2026-10-17T09:00:00Z&to=2026-10-17T10:00:00Z"), NOW)).toEqual({
      kind: "fixed",
      from: Date.parse("2026-10-17T09:00:00Z"),
      to: Date.parse("2026-10-17T10:00:00Z"),
    });
  });

  it("rejects a reversed, half or oversized fixed range", () => {
    expect(parseRange(p("from=2026-10-17T10:00:00Z&to=2026-10-17T09:00:00Z"), NOW).kind).toBe("live");
    expect(parseRange(p("from=2026-10-17T10:00:00Z"), NOW).kind).toBe("live");
    expect(parseRange(p("from=2026-08-01T00:00:00Z&to=2026-10-17T00:00:00Z"), NOW).kind).toBe("live");
    expect(parseRange(p("from=yesterday&to=today"), NOW).kind).toBe("live");
  });
});

describe("writeRange", () => {
  it("keeps other parameters and leaves the default out", () => {
    expect(writeRange(p("src=nats&range=6h"), { kind: "live", preset: "24h" }).toString()).toBe("src=nats");
  });

  it("replaces a fixed range with a preset", () => {
    const out = writeRange(p("from=2026-10-17T09:00:00Z&to=2026-10-17T10:00:00Z"), { kind: "live", preset: "7d" });
    expect(out.toString()).toBe("range=7d");
  });

  it("round-trips a fixed range at second precision", () => {
    const spec = { kind: "fixed" as const, from: NOW - 3_600_000, to: NOW };
    expect(parseRange(writeRange(p(""), spec), NOW)).toEqual(spec);
    expect(writeRange(p(""), spec).toString()).toBe("from=2026-10-17T13%3A30%3A00Z&to=2026-10-17T14%3A30%3A00Z");
  });
});

describe("live and fixed", () => {
  it("resolves a live preset against now", () => {
    expect(resolveRange({ kind: "live", preset: "1h" }, NOW)).toEqual({ start: NOW - 3_600_000, end: NOW });
  });

  it("pauses a live range at now and resumes with the nearest preset", () => {
    const fixed = pauseRange({ kind: "live", preset: "6h" }, NOW);
    expect(fixed).toEqual({ kind: "fixed", from: NOW - 6 * 3_600_000, to: NOW });
    expect(resumeLive(fixed)).toEqual({ kind: "live", preset: "6h" });
    expect(resumeLive({ kind: "fixed", from: NOW - 2 * 86_400_000, to: NOW })).toEqual({ kind: "live", preset: "24h" });
  });
});

describe("bucketFor", () => {
  it("keeps at most 120 buckets", () => {
    expect(bucketFor(3_600_000)).toBe(60_000);
    expect(bucketFor(6 * 3_600_000)).toBe(300_000);
    expect(bucketFor(24 * 3_600_000)).toBe(900_000);
    expect(bucketFor(7 * 86_400_000)).toBe(7_200_000);
  });
});

describe("rangeProblem", () => {
  const at = (q: string) => rangeProblem(p(q), NOW);

  it("accepts the default, presets and a past fixed range", () => {
    expect(at("")).toBeUndefined();
    expect(at("range=7d")).toBeUndefined();
    expect(at("from=2026-10-17T09:00:00Z&to=2026-10-17T10:00:00Z")).toBeUndefined();
    expect(at("from=2026-10-17T14:00:00Z&to=2026-10-17T15:00:00Z")).toBeUndefined();
  });

  it("names what is wrong with the range in a link", () => {
    expect(at("range=3w")).toMatch(/3w/);
    expect(at("from=2026-10-17T09:00:00Z")).toMatch(/both/);
    expect(at("from=1&to=2")).toMatch(/not valid times/);
    expect(at("from=2026-10-01&to=2026-10-02")).toMatch(/not valid times/);
    expect(at("from=2026-10-17T10:00:00Z&to=2026-10-17T09:00:00Z")).toMatch(/before/);
    expect(at("from=2026-08-01T00:00:00Z&to=2026-10-17T00:00:00Z")).toMatch(/30 days/);
    expect(at("from=2099-01-01T00:00:00Z&to=2099-01-02T00:00:00Z")).toMatch(/future/);
  });

  it("falls back to the default whenever there is a problem", () => {
    for (const q of ["from=1&to=2", "from=2099-01-01T00:00:00Z&to=2099-01-02T00:00:00Z", "range=3w"]) {
      expect(parseRange(p(q), NOW)).toEqual({ kind: "live", preset: "24h" });
    }
  });
});

// The global time range, kept in the URL so that a link shows the same view.
//
// Live: ?range=1h|6h|24h|7d (24h when absent); the end follows now.
// Fixed: ?from=<ISO>&to=<ISO>; both ends stay where they are.
import { serverNow } from "./clock";

export type Preset = "1h" | "6h" | "24h" | "7d";

export const PRESETS: { id: Preset; label: string; ms: number }[] = [
  { id: "1h", label: "Last 1h", ms: 3_600_000 },
  { id: "6h", label: "Last 6h", ms: 6 * 3_600_000 },
  { id: "24h", label: "Last 24h", ms: 24 * 3_600_000 },
  { id: "7d", label: "Last 7d", ms: 7 * 24 * 3_600_000 },
];

export const DEFAULT_PRESET: Preset = "24h";

/** How often a live range is asked again. */
export const LIVE_REFRESH_MS = 30_000;

export type RangeSpec = { kind: "live"; preset: Preset } | { kind: "fixed"; from: number; to: number };

/** Longest fixed range accepted from a URL: 30 days. */
const MAX_SPAN = 30 * 24 * 3_600_000;

function presetOf(id: string | null): Preset | undefined {
  return PRESETS.find((p) => p.id === id)?.id;
}

export function presetMs(p: Preset): number {
  return PRESETS.find((x) => x.id === p)!.ms;
}

/** A UTC time with a Z, to the minute or finer: 2026-10-17T09:00:00Z. */
const ISO = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?Z$/;

/**
 * What is wrong with the range a link gives, or undefined when nothing is.
 * A link with a problem shows the default range and says why.
 */
export function rangeProblem(params: URLSearchParams, now = serverNow()): string | undefined {
  const from = params.get("from");
  const to = params.get("to");
  if (from !== null || to !== null) {
    if (!from || !to) return "The link gives only one end of its range; it needs both from and to.";
    if (!ISO.test(from) || !ISO.test(to)) return "The link's from and to are not valid times (UTC, as 2026-10-17T09:00:00Z).";
    const f = Date.parse(from);
    const t = Date.parse(to);
    if (!(f < t)) return "The link's range does not start before it ends.";
    if (t - f > MAX_SPAN) return "The link's range is longer than 30 days.";
    if (f > now) return "The link's range lies in the future.";
    return undefined;
  }
  const r = params.get("range");
  if (r !== null && !presetOf(r)) return `The link names an unknown range "${r}".`;
  return undefined;
}

/** Reads the range from URL parameters; a range with a problem gives the default. */
export function parseRange(params: URLSearchParams, now = serverNow()): RangeSpec {
  if (rangeProblem(params, now)) return { kind: "live", preset: DEFAULT_PRESET };
  const from = params.get("from");
  const to = params.get("to");
  if (from && to) return { kind: "fixed", from: Date.parse(from), to: Date.parse(to) };
  return { kind: "live", preset: presetOf(params.get("range")) ?? DEFAULT_PRESET };
}

function iso(ms: number): string {
  // Second precision, without milliseconds: 2026-10-17T14:30:00Z.
  return new Date(Math.floor(ms / 1000) * 1000).toISOString().replace(".000Z", "Z");
}

/** Returns params with the range written into them; other parameters stay. */
export function writeRange(params: URLSearchParams, spec: RangeSpec): URLSearchParams {
  const out = new URLSearchParams(params);
  out.delete("range");
  // Collection pins belong to the previous comparison, not the new range.
  out.delete("epochA"); out.delete("epochB");
  out.delete("from");
  out.delete("to");
  if (spec.kind === "fixed") {
    out.set("from", iso(spec.from));
    out.set("to", iso(spec.to));
  } else if (spec.preset !== DEFAULT_PRESET) {
    out.set("range", spec.preset);
  }
  return out;
}

export function resolveRange(spec: RangeSpec, now: number): { start: number; end: number } {
  if (spec.kind === "fixed") return { start: spec.from, end: spec.to };
  return { start: now - presetMs(spec.preset), end: now };
}

export function rangeLabel(spec: RangeSpec): string {
  return spec.kind === "live" ? PRESETS.find((p) => p.id === spec.preset)!.label : "Custom";
}

/** Freezes a live range at now. */
export function pauseRange(spec: RangeSpec, now: number): RangeSpec {
  if (spec.kind === "fixed") return spec;
  const r = resolveRange(spec, now);
  return { kind: "fixed", from: r.start, to: r.end };
}

/** Goes live with the preset closest to the span of a fixed range. */
export function resumeLive(spec: RangeSpec): RangeSpec {
  if (spec.kind === "live") return spec;
  const span = spec.to - spec.from;
  const best = PRESETS.reduce((a, p) => (Math.abs(p.ms - span) < Math.abs(a.ms - span) ? p : a));
  return { kind: "live", preset: best.id };
}

const BUCKETS = [60_000, 300_000, 900_000, 1_800_000, 3_600_000, 7_200_000, 21_600_000, 43_200_000, 86_400_000];

/** Timeline bucket width: the smallest step that gives at most 120 buckets. */
export function bucketFor(spanMs: number): number {
  return BUCKETS.find((b) => spanMs / b <= 120) ?? BUCKETS[BUCKETS.length - 1];
}

/** Readable bucket widths, with at most one decimal: "30 s", "15 min", "2 h". */
export function bucketLabel(ms: number): string {
  const rounded = (unit: number) => Number((ms / unit).toFixed(1));
  if (ms >= 86_400_000) {
    const days = rounded(86_400_000);
    return `${days} ${days === 1 ? "day" : "days"}`;
  }
  if (ms >= 3_600_000) return `${rounded(3_600_000)} h`;
  if (ms >= 60_000) return `${rounded(60_000)} min`;
  return ms > 0 && ms < 50 ? "<0.1 s" : `${rounded(1000)} s`;
}

/** Stable query key part: live ranges by preset, fixed ranges by their ends. */
export function rangeKey(spec: RangeSpec): string {
  return spec.kind === "live" ? `live:${spec.preset}` : `fixed:${spec.from}-${spec.to}`;
}

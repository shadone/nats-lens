// Colour scales of the server map: a few bins per metric, each with the
// class that fills its square and the words its legend shows.
import type { ServerRow } from "../api/servers";
import { fmtBytes, fmtCount, fmtInt } from "./format";
import { leaders, storePct, versionCounts } from "./servers";

export type ColourBy = "cpu" | "memory" | "connections" | "store" | "leaders" | "version";

export const COLOUR_BY: { id: ColourBy; label: string }[] = [
  { id: "cpu", label: "CPU" },
  { id: "memory", label: "Memory" },
  { id: "connections", label: "Connections" },
  { id: "store", label: "Store used" },
  { id: "leaders", label: "Leaders" },
  { id: "version", label: "Version" },
];

export interface Bin {
  /** CSS class of the square: heat-1 to heat-4, or ver-0 and up. */
  cls: string;
  label: string;
}

export interface ColourScale {
  bins: Bin[];
  /** Unset when the value was not observed: the square gets no colour. */
  binOf: (s: ServerRow) => Bin | undefined;
  /** The value as the square's tooltip names it; "–" when not observed. */
  valueText: (s: ServerRow) => string;
}

const pct = (v: number) => `${fmtCount(Math.round(v))}%`;

/** Fixed percentage bins: under 10, 10 to 30, 30 to 60, 60 or more. */
const PCT_EDGES = [10, 30, 60];

/** One bin below the first edge, one between each pair, one from the last edge up; the top bin is always heat-4. */
function edgeBins(edges: number[], fmt: (v: number) => string): Bin[] {
  const n = edges.length;
  return Array.from({ length: n + 1 }, (_, i) => ({
    cls: i === n ? "heat-4" : `heat-${i + 1}`,
    label: i === 0 ? `under ${fmt(edges[0])}` : i === n ? `${fmt(edges[n - 1])} or more` : `${fmt(edges[i - 1])} to ${fmt(edges[i])}`,
  }));
}

/** `shown` turns a value into the one the tooltip shows, so "60%" bins as 60. */
function numeric(value: (s: ServerRow) => number | undefined, edges: number[], fmt: (v: number) => string, shown: (v: number) => number = (v) => v): ColourScale {
  const bins = edgeBins(edges, fmt);
  return {
    bins,
    binOf: (s) => {
      const raw = value(s);
      const v = raw === undefined ? undefined : shown(raw);
      if (v === undefined) return undefined;
      const i = edges.findIndex((e) => v < e);
      return bins[i === -1 ? edges.length : i];
    },
    valueText: (s) => {
      const v = value(s);
      return v === undefined ? "–" : fmt(v);
    },
  };
}

/**
 * Quarters of the spread between the smallest and largest value among the
 * servers, for metrics without a fixed scale. Counts get whole edges, and
 * edges that round together merge.
 */
function relative(servers: ServerRow[], value: (s: ServerRow) => number | undefined, fmt: (v: number) => string, whole = false): ColourScale {
  // A server that did not answer keeps its last numbers, which must not
  // stretch the scale of the servers that did.
  const vals = servers.filter((s) => s.present).map(value).filter((v): v is number => v !== undefined);
  const min = vals.length ? Math.min(...vals) : 0;
  const max = vals.length ? Math.max(...vals) : 0;
  const q = (max - min || (whole ? 4 : max || 4)) / 4;
  const raw = [1, 2, 3].map((k) => min + k * q).map((e) => (whole ? Math.max(1, Math.ceil(e)) : e));
  return numeric(value, [...new Set(raw)], fmt);
}

export function colourScale(by: ColourBy, servers: ServerRow[]): ColourScale {
  switch (by) {
    case "cpu":
      return numeric((s) => s.cpuPercent, PCT_EDGES, pct, Math.round);
    case "store":
      return numeric(storePct, PCT_EDGES, pct, Math.round);
    case "memory":
      return relative(servers, (s) => s.memBytes, fmtBytes);
    case "connections":
      return relative(servers, (s) => s.connections, (v) => fmtInt(v), true);
    case "leaders":
      return relative(servers, leaders, (v) => fmtInt(v), true);
    case "version": {
      const groups = versionGroups(servers);
      const bins = groups.map((g) => ({ cls: g.cls, label: g.label }));
      return {
        bins,
        binOf: (s) => {
          const g = groups.find((x) => x.versions.includes(s.version));
          return g && bins.find((b) => b.cls === g.cls);
        },
        valueText: (s) => s.version || "–",
      };
    }
  }
}

/** The most versions that get a colour of their own when there are more than four. */
const OWN_VERSION_COLOURS = 3;

export interface VersionGroup {
  cls: string;
  label: string;
  versions: string[];
  count: number;
}

/**
 * Versions by servers running them, most first, each with its colour; with
 * more than four versions, the fourth onwards share "other versions".
 */
export function versionGroups(servers: ServerRow[]): VersionGroup[] {
  const counts = versionCounts(servers);
  if (counts.length <= OWN_VERSION_COLOURS + 1) return counts.map((c, i) => ({ cls: `ver-${i}`, label: c.version, versions: [c.version], count: c.count }));
  const own = counts.slice(0, OWN_VERSION_COLOURS).map((c, i) => ({ cls: `ver-${i}`, label: c.version, versions: [c.version], count: c.count }));
  const rest = counts.slice(OWN_VERSION_COLOURS);
  return [...own, { cls: `ver-${OWN_VERSION_COLOURS}`, label: "other versions", versions: rest.map((c) => c.version), count: rest.reduce((a, c) => a + c.count, 0) }];
}

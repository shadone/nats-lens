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

function numeric(value: (s: ServerRow) => number | undefined, edges: number[], fmt: (v: number) => string): ColourScale {
  const bins = edgeBins(edges, fmt);
  return {
    bins,
    binOf: (s) => {
      const v = value(s);
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
  const vals = servers.map(value).filter((v): v is number => v !== undefined);
  const min = vals.length ? Math.min(...vals) : 0;
  const max = vals.length ? Math.max(...vals) : 0;
  const q = (max - min || (whole ? 4 : max || 4)) / 4;
  const raw = [1, 2, 3].map((k) => min + k * q).map((e) => (whole ? Math.max(1, Math.ceil(e)) : e));
  return numeric(value, [...new Set(raw)], fmt);
}

export function colourScale(by: ColourBy, servers: ServerRow[]): ColourScale {
  switch (by) {
    case "cpu":
      return numeric((s) => s.cpuPercent, PCT_EDGES, pct);
    case "store":
      return numeric(storePct, PCT_EDGES, pct);
    case "memory":
      return relative(servers, (s) => s.memBytes, fmtBytes);
    case "connections":
      return relative(servers, (s) => s.connections, (v) => fmtInt(v), true);
    case "leaders":
      return relative(servers, leaders, (v) => fmtInt(v), true);
    case "version": {
      const versions = versionCounts(servers);
      const bins = versions.map((v, i) => ({ cls: `ver-${Math.min(i, 3)}`, label: v.version }));
      return {
        bins,
        binOf: (s) => bins.find((b) => b.label === s.version),
        valueText: (s) => s.version || "–",
      };
    }
  }
}

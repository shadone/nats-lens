// Formats a metric value by its unit.
import type { Unit } from "../api/model";
import { fmtBytes, fmtCount, fmtDuration, splitBytes } from "./format";

export function fmtValue(v: number, unit: Unit): string {
  switch (unit) {
    case "bytes":
      return fmtBytes(v);
    case "bytes_per_day":
      return `${fmtBytes(v)}/day`;
    case "bytes_per_second":
      return `${fmtBytes(v)}/s`;
    case "per_second":
      return `${fmtCount(v)}/s`;
    case "percent":
      return `${fmtCount(v)}%`;
    case "seconds":
      return fmtDuration(v * 1000);
    default:
      return fmtCount(v);
  }
}

/** A tile's big number and the unit that goes before its suffix. */
export function tileValue(v: number | undefined, unit: Unit): [string, string] {
  if (v === undefined) return ["–", ""];
  if (unit === "bytes") {
    // One decimal at most, as the reference shows 5.8 GiB.
    const [n, u] = splitBytes(v);
    return [String(Math.round(Number(n) * 10) / 10), u];
  }
  if (unit === "percent") return [fmtCount(v), "%"];
  return [fmtCount(v), ""];
}

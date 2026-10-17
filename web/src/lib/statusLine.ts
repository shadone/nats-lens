// The Overview's one-line status: how the system serves, from the worst
// unmuted finding and, in a live range, how fresh the newest epoch is.
import type { Freshness, Level, SeverityLevel, Tally } from "../api/model";

export interface StatusLine {
  level: Level;
  text: string;
  /** Unmuted firing findings per severity, worst first; severities without any are left out. */
  badges: { level: SeverityLevel; count: number }[];
}

/** Whether a live range's newest epoch is too old, or no collector writes. */
export function staleData(f: Freshness, now: number): boolean {
  if (!f.live || f.epochAt === undefined) return true;
  return now - f.epochAt > 3 * f.intervalMs;
}

export function statusLine(t: Tally, f: Freshness | undefined, live: boolean, now: number): StatusLine {
  const badges = (["critical", "warning", "info"] as const).filter((l) => t[l] > 0).map((l) => ({ level: l, count: t[l] }));
  if (!f || f.epochAt === undefined) return { level: "nodata", text: "No data yet", badges };
  if (live && staleData(f, now)) return { level: "nodata", text: "Not collecting", badges };
  if (t.critical > 0) return { level: "critical", text: "Critical findings", badges };
  if (t.warning > 0) return { level: "warning", text: "Needs attention", badges };
  return { level: "ok", text: "Serving normally", badges };
}

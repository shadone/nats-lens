// The Overview's one-line status: how the system serves, from the worst
// unmuted finding and, in a live range, how fresh the newest epoch is.
import type { Freshness, Level, SeverityLevel, Tally } from "../api/model";
import { dayMonHm, hm } from "./format";

export interface StatusLine {
  level: Level;
  text: string;
  /** Unmuted firing findings per severity, worst first; severities without any are left out. */
  badges: { level: SeverityLevel; count: number }[];
}

/** Whether a live range's newest collection is too old, or no collector writes or collects. */
export function staleData(f: Freshness, now: number): boolean {
  if (!f.live || !f.collecting || f.epochAt === undefined) return true;
  return now - f.epochAt > 3 * f.intervalMs;
}

export function statusLine(t: Tally, f: Freshness | undefined, live: boolean, now: number): StatusLine {
  const badges = (["critical", "warning", "info"] as const).filter((l) => t[l] > 0).map((l) => ({ level: l, count: t[l] }));
  if (live && f && !f.collecting) {
    // The header already says since when; this line names the last data.
    const at = f.epochAt;
    const text = at !== undefined ? `Not collecting · last data ${now - at < 86_400_000 ? hm(at) : dayMonHm(at)}` : "Not collecting";
    return { level: "nodata", text, badges };
  }
  if (!f || f.epochAt === undefined) return { level: "nodata", text: "No data yet", badges };
  if (live && staleData(f, now)) return { level: "nodata", text: "Not collecting", badges };
  if (t.critical > 0) return { level: "critical", text: "Critical findings", badges };
  if (t.warning > 0) return { level: "warning", text: "Needs attention", badges };
  return { level: "ok", text: "Serving normally", badges };
}

/**
 * The firing findings kept out of the status, plain mutes apart from
 * acceptances: "2 muted · 3 accepted", "2 muted", "3 accepted"; empty
 * when there are none. The tally counts acceptances within muted.
 */
export function quietCount(t: Tally): string {
  const accepted = Math.min(t.accepted ?? 0, t.muted);
  const muted = t.muted - accepted;
  return [muted > 0 ? `${muted} muted` : "", accepted > 0 ? `${accepted} accepted` : ""].filter(Boolean).join(" · ");
}

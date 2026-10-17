// The Fleet page's rules: each system's state, the order of the cards,
// the headline, the freshness line and the sidebar badge.
import type { Level, SystemSummary } from "../api/model";
import type { Badge } from "../components/Sidebar";
import { dayMonHm, fmtDuration, hm } from "./format";
import { PRESETS } from "./timeRange";
import { entityName } from "./view";

/** A card's state: the status, unless the system is not collecting or its summary failed. */
export type CardState = "critical" | "warning" | "info" | "notcollecting" | "unavailable" | "nodata" | "muted" | "ok";

/** Worst first; a lower number sorts first. */
const ORDER: Record<CardState, number> = { critical: 0, warning: 1, info: 2, notcollecting: 3, unavailable: 4, nodata: 5, muted: 6, ok: 7 };

export const CARD_STATE_LABEL: Record<CardState, string> = {
  critical: "critical",
  warning: "warning",
  info: "info",
  notcollecting: "not collecting",
  unavailable: "summary unavailable",
  nodata: "no data",
  muted: "muted",
  ok: "ok",
};

export function notCollecting(s: SystemSummary): boolean {
  return s.collection === "not connected" || s.collection === "connection closed" || s.collectionError !== "";
}

export function cardState(s: SystemSummary): CardState {
  if (s.error) return "unavailable";
  if (notCollecting(s)) return "notcollecting";
  return s.status;
}

/** The systems worst first; systems in the same state keep their configuration order. */
export function worstFirst(systems: SystemSummary[]): SystemSummary[] {
  return systems
    .map((s, i) => ({ s, i, o: ORDER[cardState(s)] }))
    .sort((a, b) => a.o - b.o || a.i - b.i)
    .map((x) => x.s);
}

const DAY = 86_400_000;

function since(t: number, now: number): string {
  return `${Math.abs(now - t) < DAY ? hm(t) : dayMonHm(t)} UTC`;
}

export interface Headline {
  text: string;
  /** Firing findings besides the one named. */
  more: number;
}

/** The card's headline: the collection error, the worst finding, or what holds instead. */
export function headline(s: SystemSummary, now: number): Headline {
  const firing = s.tally.critical + s.tally.warning + s.tally.info;
  switch (cardState(s)) {
    case "unavailable":
      return { text: s.error, more: 0 };
    case "notcollecting": {
      const what = s.collectionError || (s.collection === "connection closed" ? "Connection closed" : "Not connected");
      return { text: s.collectionErrorSince !== undefined ? `${what} since ${since(s.collectionErrorSince, now)}` : what, more: 0 };
    }
    case "nodata":
      return { text: "No collection in this range", more: 0 };
    case "muted":
      return { text: "Every firing finding is muted", more: 0 };
    case "ok":
      return { text: "No findings firing", more: 0 };
    default:
      if (!s.worst) return { text: `${firing} findings firing`, more: 0 };
      return { text: `${s.worst.checkName}: ${entityName(s.worst.entity) || "system"}`, more: Math.max(0, firing - 1) };
  }
}

/**
 * The newest epoch's age: live while it is within three collect intervals
 * of now and the system collects, else when data last arrived. A fixed
 * range gives the newest epoch's time.
 */
export function freshnessText(s: SystemSummary, live: boolean, now: number): string {
  const f = s.freshness;
  if (!f || f.epochAt === undefined) return "no data";
  if (!live) return `newest ${dayMonHm(f.epochAt)}`;
  const age = Math.max(0, now - f.epochAt);
  if (f.live && !notCollecting(s) && age <= 3 * f.intervalMs) return `live · ${fmtDuration(age)}`;
  return `last data ${lastDataWhen(f.epochAt, live, now)}`;
}

/** When the newest epoch started: "3h ago" in a live range, its time in a fixed one. */
function lastDataWhen(epochAt: number, live: boolean, now: number): string {
  return live ? `${fmtDuration(Math.max(0, now - epochAt))} ago` : dayMonHm(epochAt);
}

/**
 * The caption over the counts of a system that is not collecting, whose
 * numbers are those of its newest epoch: "At last data, 3h ago". Undefined
 * while the system collects or has no epoch.
 */
export function staleCaption(s: SystemSummary, live: boolean, now: number): string | undefined {
  const at = s.freshness?.epochAt;
  if (cardState(s) !== "notcollecting" || at === undefined) return undefined;
  return `At last data, ${lastDataWhen(at, live, now)}`;
}

/**
 * The strip's left end, from the data itself, so a strip of the previous
 * range keeps its own label while the next range loads: "24h ago" for a
 * live strip, its start time for a fixed one.
 */
export function stripStartLabel(s: SystemSummary, live: boolean): string {
  if (s.stripStart === undefined) return "";
  if (!live) return dayMonHm(s.stripStart);
  const span = s.strip.length * (s.stripBucketMs ?? 0);
  const preset = PRESETS.find((p) => p.ms === span);
  return `${preset ? preset.id : fmtDuration(span)} ago`;
}

const BADGE_LEVEL: Partial<Record<CardState, Level>> = { critical: "critical", warning: "warning", info: "info" };

/** The sidebar's Fleet badge: systems that are not OK, at the level of the worst. */
export function fleetBadge(systems: SystemSummary[] | undefined): Badge | undefined {
  const bad = (systems ?? []).map(cardState).filter((c) => c !== "ok" && c !== "muted");
  if (!bad.length) return undefined;
  const worst = bad.reduce((a, b) => (ORDER[b] < ORDER[a] ? b : a));
  return { count: bad.length, level: BADGE_LEVEL[worst] ?? "nodata", what: "not OK", worst: CARD_STATE_LABEL[worst] };
}

// The Data history page's wording and order, apart from React so they can
// be tested on their own: spans, times, gap labels, the uses of history,
// the archive and the summary line.
import { isHatched, type ArchiveState, type DataHistory, type GapKind, type HistoryCoverage, type HistoryGap, type HistoryUse } from "../api/history";
import { dayMonHm, eventWhen, fmtInt, hm, isoDate, isoMinute } from "./format";

const SEC = 1000;
const MIN = 60 * SEC;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

export const HISTORY_SUBTITLE = "What nats-lens holds of this system, and where it has gaps.";

export const ARCHIVE_NOTE =
  "The archive holds hourly Parquet files of every table. The UI reads the database only; hours older than the retention are in the archive, for `natslens sql` after a rebuild or for other tools.";

function unit(n: number, short: string, long: string, plural = `${long}s`, asLong = false): string {
  if (!asLong) return `${n} ${short}`;
  return `${n} ${n === 1 ? long : plural}`;
}

/**
 * A span in its two largest units: "45 s", "31 min", "4 min 10 s",
 * "5 h 29 min", "6 h", "13 d 6 h", "30 d"; long: "13 days 6 hours".
 */
export function spanText(ms: number, long = false): string {
  const s = Math.max(0, Math.round(ms / SEC));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const u = (n: number, short: string, word: string) => unit(n, short, word, `${word}s`, long);
  const two = (a: string, bn: number, b: string) => (bn > 0 ? `${a} ${b}` : a);
  if (d > 0) return two(u(d, "d", "day"), h, u(h, "h", "hour"));
  if (h > 0) return two(u(h, "h", "hour"), m, unit(m, "min", "min", "min", long));
  if (m > 0) return two(unit(m, "min", "min", "min", long), sec, unit(sec, "s", "s", "s", long));
  return unit(sec, "s", "s", "s", long);
}

/** "13:47 UTC" on now's UTC day, else "2026-10-22 09:00 UTC". */
export function whenText(at: number, now: number): string {
  return `${isoDate(at) === isoDate(now) ? hm(at) : isoMinute(at)} UTC`;
}

/** A time in the gaps table: 17:50:44 within a day of the range's end, else 16 Oct 09:31. */
export function gapTime(at: number, rangeEnd: number): string {
  return eventWhen(at, rangeEnd);
}

const GAP_LABEL: Record<GapKind, string> = {
  notRunning: "nats-lens was not running",
  notConnected: "nats-lens could not reach NATS",
  noCollections: "No collections",
  eventsNotCaptured: "Events not captured",
  kubernetesNotWatched: "Kubernetes not watched",
  beforeHistory: "nats-lens was not running yet",
  unknown: "Gap of an unknown kind",
};

export function gapLabel(kind: GapKind): string {
  return GAP_LABEL[kind];
}

/** What else a row says: the connection lost, and that collections went on for the kinds charts do not hatch. */
export function gapDetail(g: HistoryGap): string | undefined {
  if (g.kind === "eventsNotCaptured") return g.connection ? `connection ${g.connection}; collections went on` : "collections went on";
  if (g.kind === "kubernetesNotWatched") return "collections went on";
  return undefined;
}

/** The end of a gap: its time, else "still now" on a live range, "after the range" on a fixed one. */
export function gapEndText(g: HistoryGap, rangeEnd: number, live: boolean): string {
  if (g.end !== undefined) return gapTime(g.end, rangeEnd);
  return live ? "still now" : "after the range";
}

/** How long a gap lasted; an open one up to the range's end, "so far" or "at least". */
export function gapLengthText(g: HistoryGap, rangeEnd: number, live: boolean): string | undefined {
  if (g.start === undefined) return undefined;
  if (g.end !== undefined) return spanText(g.end - g.start);
  const span = spanText(Math.max(0, rangeEnd - g.start));
  return live ? `${span} so far` : `at least ${span}`;
}

/** "No data 17:50:44–17:53:44: nats-lens was not running", the name of a hatched stretch. */
export function gapName(g: HistoryGap, rangeEnd: number, live: boolean): string {
  const from = g.start === undefined ? "" : gapTime(g.start, rangeEnd);
  return `No data ${from}–${gapEndText(g, rangeEnd, live)} UTC: ${gapLabel(g.kind)}`;
}

/** "Showing 200 of 243 times without collections", for each class of gap cut at 200. */
export function gapCountLines(d: Pick<DataHistory, "gaps" | "gapsTotal" | "listedGapsTotal">): string[] {
  const hatched = d.gaps.filter(isHatched).length;
  const other = d.gaps.length - hatched;
  const out: string[] = [];
  if (hatched < d.gapsTotal) out.push(`Showing ${fmtInt(hatched)} of ${fmtInt(d.gapsTotal)} times without collections, the newest.`);
  if (other < d.listedGapsTotal) out.push(`Showing ${fmtInt(other)} of ${fmtInt(d.listedGapsTotal)} times without events or Kubernetes, the newest.`);
  return out;
}

export interface StripSegment {
  gap: HistoryGap;
  /** Percent of the range from its start. */
  left: number;
  width: number;
}

/** The hatched gaps placed on the range, cut to it; an open gap runs to the range's end. */
export function stripSegments(d: Pick<DataHistory, "gaps" | "rangeStart" | "rangeEnd">): StripSegment[] {
  const span = d.rangeEnd - d.rangeStart;
  if (span <= 0) return [];
  const pct = (t: number) => (Math.min(d.rangeEnd, Math.max(d.rangeStart, t)) - d.rangeStart) / span * 100;
  return d.gaps
    .filter((g) => isHatched(g) && g.start !== undefined)
    .map((g) => {
      const left = pct(g.start!);
      const right = pct(g.end ?? d.rangeEnd);
      return { gap: g, left: round(left), width: round(right - left) };
    })
    .reverse();
}

function round(v: number): number {
  return Math.round(v * 100) / 100;
}

/** The strip's two ends: "13:47" and "now", or with the day over a day or more. */
export function stripEnds(d: Pick<DataHistory, "rangeStart" | "rangeEnd">, live: boolean): [string, string] {
  const fmt = d.rangeEnd - d.rangeStart >= DAY ? dayMonHm : hm;
  return [fmt(d.rangeStart), live ? "now" : fmt(d.rangeEnd)];
}

/** The share of the covered span with collections; undefined without history in the range. */
export function coveredShare(c: HistoryCoverage): number | undefined {
  if (c.from === undefined || c.to === undefined || c.withoutData === undefined || c.to <= c.from) return undefined;
  return Math.max(0, 1 - c.withoutData / (c.to - c.from));
}

/** "66%"; never "100%" with a gap, never "0%" with any data. */
export function shareText(share: number, withoutData: number): string {
  if (withoutData === 0) return "100%";
  const p = share * 100;
  if (p > 0 && p < 1) return "<1%";
  return `${Math.min(99, Math.floor(p))}%`;
}

/** "13:47–22:47 UTC", the covered span, with dates when it spans more than a day. */
export function coveredSpanText(c: HistoryCoverage): string | undefined {
  if (c.from === undefined || c.to === undefined) return undefined;
  if (c.to - c.from < DAY) return `${hm(c.from)}–${hm(c.to)} UTC`;
  return `${isoMinute(c.from)} to ${isoMinute(c.to)} UTC`;
}

export interface UseLabel {
  title: string;
  /** What reads it. */
  sub: string;
}

/** The name of a use of history and what reads it. */
export function historyUseLabel(u: Pick<HistoryUse, "id" | "checkCode">): UseLabel {
  const [kind] = u.id.split(":");
  switch (kind) {
    case "growth":
      return { title: "Growth per day", sub: "Streams, Capacity, the storage tile; over the range" };
    case "forecast":
      if (u.checkCode === "server-014") return { title: "Store forecast", sub: `${u.checkCode}: when a server's store fills` };
      if (u.checkCode === "stream-006") return { title: "Stream forecast", sub: `${u.checkCode}: when a stream reaches its byte limit` };
      return { title: "Forecast", sub: u.checkCode };
    case "lookback":
      return { title: `Check ${u.checkCode}`, sub: "its lookback" };
    case "accept_review":
      return { title: "Growth review of accepted findings", sub: "8 days and an hour, kept as long" };
    case "day":
      return { title: "The last 24 hours", sub: "Applications traffic, failed logins (Accounts), drops (Topology)" };
    case "replay":
      return { title: "Replay", sub: "moments within the history" };
    default:
      return { title: u.id, sub: u.checkCode };
  }
}

export type UseTone = "ok" | "warn" | "muted";

/** Whether a use has enough history, and when it will: "enough", "enough at 15:02 UTC", "never with retention 7 d". */
export function enoughState(u: HistoryUse, retention: number | undefined, now: number): { text: string; tone: UseTone } {
  if (u.enough) return { text: "enough", tone: "ok" };
  if (u.never) return { text: retention === undefined ? "never with this retention" : `never with retention ${spanText(retention)}`, tone: "warn" };
  if (u.enoughAt !== undefined) return { text: `enough at ${whenText(u.enoughAt, now)}`, tone: "muted" };
  return { text: "not within this range", tone: "muted" };
}

/** "needs 6 h; has 31 min; enough at 15:02 UTC" */
export function historyUseLine(u: HistoryUse, retention: number | undefined, now: number): string {
  const needs = u.needs === undefined ? "needs –" : `needs ${spanText(u.needs)}`;
  const has = u.has === undefined ? "has none" : `has ${spanText(u.has)}`;
  return `${needs}; ${has}; ${enoughState(u, retention, now).text}`;
}

/** "Collections and events are kept for 14 days: …", the database's retention. */
export function retentionLine(d: Pick<DataHistory, "retention" | "retentionFrom">, now: number): string {
  if (d.retention === undefined) return "Collections and events are kept forever: no retention is set.";
  const kept = `Collections and events are kept for ${spanText(d.retention, true)}`;
  if (d.retentionFrom === undefined) return `${kept}.`;
  return `${kept}: the hourly sweep removes what is older than ${whenText(d.retentionFrom, now)}.`;
}

/** "2,161, from 2026-08-07 22:00 to 2026-11-05 22:00 UTC" */
export function archiveHoursText(a: ArchiveState): string {
  if (a.hours === undefined) return "not listed yet; the archiver lists its hours once an hour after an export";
  if (a.hours === 0 || a.oldestHour === undefined || a.newestHour === undefined) return a.hours === 0 ? "none yet" : fmtInt(a.hours);
  return `${fmtInt(a.hours)}, from ${isoMinute(a.oldestHour)} to ${isoMinute(a.newestHour)} UTC`;
}

export function archiveKeptText(a: ArchiveState): string {
  return a.retention === undefined ? "every hour" : spanText(a.retention, true);
}

/** "22:44 UTC, the open hour provisionally", or after it closed. */
export function lastExportText(a: ArchiveState, now: number): string {
  if (a.lastExportAt === undefined) return "none yet";
  const when = whenText(a.lastExportAt, now);
  return a.newestFinal ? `${when}, the newest hour after it closed` : `${when}, the open hour provisionally`;
}

/** "Last export failed at 21:00 UTC: …"; undefined without an error. */
export function lastErrorText(a: ArchiveState, now: number): string | undefined {
  if (!a.lastError) return undefined;
  return a.lastErrorAt === undefined ? `Last export failed: ${a.lastError}` : `Last export failed at ${whenText(a.lastErrorAt, now)}: ${a.lastError}`;
}

/** "4 findings were already true when the history begins, at 13:47 UTC." */
export function findingsAtStartText(n: number, historyStart: number, now: number): string {
  const at = whenText(historyStart, now);
  if (n === 0) return `No finding was already true when the history begins, at ${at}.`;
  return `${fmtInt(n)} ${n === 1 ? "finding was" : "findings were"} already true when the history begins, at ${at}.`;
}

/**
 * The line under the header: "Collecting since 2026-10-22 09:00 UTC
 * (13 days 6 hours) · kept for 14 days · archived for 90 days", with
 * the stop of collection and the oldest event when they apply.
 */
export function summaryParts(d: Pick<DataHistory, "historyStart" | "eventsStart" | "retention" | "archive" | "intervalMs">, now: number, notCollectingSince?: number): string[] {
  if (d.historyStart === undefined) {
    const out = ["No collection stored yet"];
    if (notCollectingSince !== undefined) out.push(`not collecting since ${whenText(notCollectingSince, now)}`);
    return out;
  }
  const lead = notCollectingSince === undefined ? "Collecting since" : "History since";
  const out = [`${lead} ${whenText(d.historyStart, now)} (${spanText(now - d.historyStart, true)})`];
  if (notCollectingSince !== undefined) out.push(`not collecting since ${whenText(notCollectingSince, now)}`);
  out.push(d.retention === undefined ? "kept forever" : `kept for ${spanText(d.retention, true)}`);
  if (!d.archive.enabled) out.push("no archive");
  else out.push(d.archive.retention === undefined ? "archived, every hour kept" : `archived for ${spanText(d.archive.retention, true)}`);
  const slack = d.intervalMs ?? 0;
  if (d.eventsStart !== undefined && Math.abs(d.eventsStart - d.historyStart) > slack) out.push(`events from ${whenText(d.eventsStart, now)}`);
  return out;
}

// Words and numbers of the Capacity page, from the view model of
// api/capacity.ts. A value the server left unset gives an en dash or no
// words, never 0.
import type { Capacity, CapacityStream, ServerStore } from "../api/capacity";
import { fmtBytes, fmtGrowth, fmtInt, isoDate, isoMinute } from "./format";

export const DASH = "–";

export const CAPACITY_SUBTITLE = "What each stream keeps, what bounds it, and when the disks fill.";
const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const YEAR_DAYS = 365.25;

/** The check whose finding the page offers to accept, and its name in checks/stream-005.sql. */
export const ACCEPT_CHECK = { code: "stream-005", name: "Stream without any limit" };

/** The cards by cluster, in the order the server gave them; the page names a group only when there is more than one. */
export function clusterGroups(servers: ServerStore[]): { cluster: string; servers: ServerStore[] }[] {
  const groups: { cluster: string; servers: ServerStore[] }[] = [];
  for (const s of servers) {
    const g = groups.find((x) => x.cluster === s.cluster);
    if (g) g.servers.push(s);
    else groups.push({ cluster: s.cluster, servers: [s] });
  }
  return groups;
}

function trim1(v: number): string {
  const s = v.toFixed(1);
  return s.endsWith(".0") ? s.slice(0, -2) : s;
}

function plural(n: string, one: string, many: string): string {
  return `${n} ${n === "1" ? one : many}`;
}

/** A span of days in words: "5 hours", "2.8 days", "41 days", "4 months", "3 years", "more than 5 years". */
export function daysWords(days: number): string {
  if (days < 1) return plural(String(Math.max(1, Math.round(days * 24))), "hour", "hours");
  if (days < 10) return plural(trim1(days), "day", "days");
  if (days < 60) return plural(String(Math.round(days)), "day", "days");
  if (days < 730) return plural(String(Math.round(days / (YEAR_DAYS / 12))), "month", "months");
  if (days <= 5 * YEAR_DAYS) return plural(String(Math.round(days / YEAR_DAYS)), "year", "years");
  return "more than 5 years";
}

/** A share in percent: "0.5%", "3.9%", "71%". */
export function fmtShare(pct: number): string {
  return pct < 10 ? `${trim1(pct)}%` : `${Math.round(pct)}%`;
}

export interface StoreUse {
  /** "244 MiB" */
  used: string;
  /** "of 50 GiB · 0.5%", or "no limit reported". */
  of: string;
  /** 0 to 100 for the bar; undefined without a limit. */
  pct?: number;
}

/** A reported card's use against its limit; undefined when not reported. */
export function storeUse(s: ServerStore): StoreUse | undefined {
  if (!s.reported || s.used === undefined) return undefined;
  if (s.limit === undefined || s.limit <= 0) return { used: fmtBytes(s.used), of: "no limit reported" };
  const pct = (s.used / s.limit) * 100;
  return { used: fmtBytes(s.used), of: `of ${fmtBytes(s.limit)} · ${fmtShare(pct)}`, pct: Math.min(100, Math.max(0, pct)) };
}

/** The bar's colour: the firing finding's severity, none without one. */
export function storeTone(s: ServerStore): "warn" | "crit" | undefined {
  if (!s.findingId) return undefined;
  return s.findingSeverity === "critical" ? "crit" : s.findingSeverity === "warning" ? "warn" : undefined;
}

/**
 * When the store fills at the card's growth, in words; empty when the card
 * has a rate but no limit. Without a rate, wait says how long the range's
 * history still needs ("needs 6 h of history; 5 h 29 min to go").
 */
export function timeToFull(s: ServerStore, wait?: string): string {
  if (s.perDay === undefined) return wait ? `no growth rate yet: ${wait}` : "not enough history for a rate (6 hours needed)";
  if (s.perDay <= 0) return "not growing";
  if (s.daysToFull === undefined) return "";
  if (s.daysToFull > 5 * YEAR_DAYS) return "full in more than 5 years at this rate";
  return `full in ${daysWords(s.daysToFull)}`;
}

/** "+17 MiB/day · full in more than 5 years at this rate"; undefined for a card not reported. */
export function growthLine(s: ServerStore, wait?: string): string | undefined {
  if (!s.reported) return undefined;
  const ttf = timeToFull(s, wait);
  if (s.perDay === undefined) return ttf;
  return [`${fmtGrowth(s.perDay)}/day`, ttf].filter(Boolean).join(" · ");
}

/** The finding link's words: "store full soon: 14 days by server-014". */
export function storeFindingText(s: ServerStore): string {
  if (s.findingCode === "server-014")
    return s.findingDays !== undefined ? `store full soon: ${daysWords(s.findingDays)} by server-014` : "store full soon by server-014";
  if (s.findingCode === "server-007") return "store usage high by server-007";
  return s.findingCode;
}

/** An age limit: "7 days", "48 h", "2 min". Under 3 days in hours, as the board writes a 24-hour work queue. */
export function fmtAge(ms: number): string {
  if (ms % DAY === 0 && ms >= 3 * DAY) return plural(String(ms / DAY), "day", "days");
  if (ms >= HOUR && ms % HOUR === 0) return `${ms / HOUR} h`;
  if (ms >= HOUR) return `${trim1(ms / HOUR)} h`;
  if (ms >= MINUTE && ms % MINUTE === 0) return `${ms / MINUTE} min`;
  return `${Math.round(ms / 1000)} s`;
}

/** A message limit: 1M, 100k, 96,500. */
export function fmtLimitCount(n: number): string {
  if (n >= 1e6 && n % 1e6 === 0) return `${n / 1e6}M`;
  if (n >= 1e3 && n % 1e3 === 0) return `${n / 1e3}k`;
  return fmtInt(n);
}

const RETENTION: Record<string, string> = { limits: "limits", interest: "interest", workqueue: "work queue" };

/** "R3 · limits · 48,211 msgs", "R3 · KV · 1 per key", "R3 · mirror of order-events", "R1 · KV · 1 per key · memory". */
export function kindLine(s: CapacityStream): string {
  const parts = [`R${s.replicas}`];
  if (s.kind === "kv") {
    parts.push("KV");
    if (s.maxMsgsPerSubject !== undefined) parts.push(`${fmtInt(s.maxMsgsPerSubject)} per key`);
  } else if (s.kind === "object") {
    parts.push("object store");
  } else {
    if (s.mirror) parts.push(`mirror of ${s.mirror}`);
    else if (s.sources > 0) parts.push(`sources ${plural(String(s.sources), "stream", "streams")}`);
    else if (s.retention) parts.push(RETENTION[s.retention] ?? s.retention);
    if (s.messages !== undefined && s.messages > 0) parts.push(`${fmtInt(s.messages)} msgs`);
  }
  if (s.storage === "memory") parts.push("memory");
  return parts.join(" · ");
}

/** The limits that are set, in the order age, messages, bytes, per subject; "nothing (keeps all)" without one. */
export function boundText(s: CapacityStream): string {
  const msgs = s.maxMsgs !== undefined ? `${fmtLimitCount(s.maxMsgs)} msgs` : undefined;
  const bytes = s.maxBytes !== undefined ? fmtBytes(s.maxBytes) : undefined;
  if (s.kind === "kv" || s.kind === "object") {
    const ttl = s.maxAge !== undefined ? `TTL ${fmtAge(s.maxAge)}` : undefined;
    const others = [msgs, bytes].filter((x): x is string => x !== undefined);
    let parts: string[];
    if (ttl) parts = [ttl, ...others];
    else if (others.length) parts = [...others, "no TTL"];
    else parts = [s.kind === "kv" ? "keys (no TTL)" : "nothing (keeps all)"];
    if (s.allowMsgTtl) parts.push(s.kind === "kv" ? "per-key TTLs allowed" : "per-message TTLs allowed");
    return parts.join(", ");
  }
  const parts = [
    s.maxAge !== undefined ? fmtAge(s.maxAge) : undefined,
    msgs,
    bytes,
    s.maxMsgsPerSubject !== undefined ? `${fmtLimitCount(s.maxMsgsPerSubject)} msgs per subject` : undefined,
  ].filter((x): x is string => x !== undefined);
  if (!parts.length) parts.push("nothing (keeps all)");
  if (s.allowMsgTtl) parts.push("per-message TTLs allowed");
  return parts.join(", ");
}

/** Whether any of age, messages or bytes bounds the stream; a per-subject limit alone does not. */
export function isBounded(s: CapacityStream): boolean {
  return s.maxAge !== undefined || s.maxMsgs !== undefined || s.maxBytes !== undefined;
}

export interface DataHeld {
  /** "7 days", "since 2026-02-11", "empty", "214 keys", or an en dash. */
  label: string;
  /** Width of the bar from the right, 0 to 100, on a one-year scale. */
  pct: number;
  /** The accent for a bounded stream, the info colour for an unbounded one. */
  bounded: boolean;
}

/** The oldest retained message to the collection on a one-year linear scale, full beyond a year. */
export function dataHeld(s: CapacityStream, epochAt: number): DataHeld {
  const bounded = isBounded(s);
  if (s.messages === 0) return { label: "empty", pct: 0, bounded };
  if (s.firstTs === undefined) return { label: s.kind === "kv" && s.keys !== undefined ? keysText(s.keys) : DASH, pct: 0, bounded };
  const held = Math.max(0, epochAt - s.firstTs);
  const pct = Math.min(100, (held / (365 * DAY)) * 100);
  let label: string;
  if (s.kind === "kv" && s.keys !== undefined) label = keysText(s.keys);
  else if (s.maxAge !== undefined && held >= 0.95 * s.maxAge) label = fmtAge(s.maxAge);
  else label = `since ${isoDate(s.firstTs)}`;
  return { label, pct, bounded };
}

function keysText(n: number): string {
  return `${fmtInt(n)} ${n === 1 ? "key" : "keys"}`;
}

/** The disk the stream takes over all its replicas; undefined when its bytes are not reported. */
export function sizeTimesR(s: CapacityStream): number | undefined {
  return s.bytes === undefined ? undefined : s.bytes * Math.max(1, s.replicas);
}

/** Growth over all replicas, per day; undefined under 6 hours of data. */
export function perDayTimesR(s: CapacityStream): number | undefined {
  return s.bytesPerDay === undefined ? undefined : s.bytesPerDay * Math.max(1, s.replicas);
}

function limitLabel(s: CapacityStream): string {
  switch (s.next.limit) {
    case "age":
      return s.maxAge !== undefined ? fmtAge(s.maxAge) : "age";
    case "messages":
      return s.maxMsgs !== undefined ? `${fmtLimitCount(s.maxMsgs)} msgs` : "messages";
    case "bytes":
      return s.maxBytes !== undefined ? fmtBytes(s.maxBytes) : "bytes";
    default:
      return "";
  }
}

/** The next-limit cell: its words, critical when the stream refuses messages now. */
export function nextLimitText(s: CapacityStream): { text: string; crit: boolean } {
  const n = s.next;
  if (n.limit === "none") return { text: "none: grows until the store is full", crit: false };
  if (n.limit === "empty") return { text: "empty", crit: false };
  if (n.limit === "perSubject" || n.state === "notForecast") return { text: "per subject only: not forecast", crit: false };
  const label = limitLabel(s);
  switch (n.state) {
    case "steady":
      return { text: `steady at ${label}`, crit: false };
    case "refusing":
      return { text: `${label}: reached, refuses new messages`, crit: true };
    case "reaches":
      return {
        text: `${label}: in ${n.days !== undefined ? daysWords(n.days) : "an unknown time"}${n.discardNew ? ", then refuses new messages" : ""}`,
        crit: false,
      };
    case "notReached":
      return { text: `${label}: not reached`, crit: false };
    default:
      return { text: DASH, crit: false };
  }
}

/** Words under a declared row. */
export function declaredText(reason: string): string {
  if (reason === "inventory") return "declared in the inventory";
  if (reason === "writer_ttls") return "bounded by its writers' TTLs";
  return "declared";
}

/** "muted until 2026-11-17 21:57 UTC" */
export function mutedUntil(expiresAt: number | undefined): string {
  return expiresAt !== undefined ? `muted until ${isoMinute(expiresAt)} UTC` : "muted";
}

/** A fills-soon row's tag: the check's own days, else the page's forecast. */
export function fillsSoonText(s: CapacityStream): string {
  const days = s.findingDays ?? (s.next.state === "reaches" ? s.next.days : undefined);
  return days !== undefined ? `full in ${daysWords(days)}` : "full soon";
}

/** Whether any card or row has a growth rate: under 6 hours of history none has. */
export function hasRate(d: Capacity): boolean {
  return d.servers.some((s) => s.perDay !== undefined) || d.streams.some((s) => s.bytesPerDay !== undefined);
}

/** The header line: what the page covers, and since when growth is read, or that no rate exists yet and, with wait, how long it needs. */
export function headerLine(d: Capacity, wait?: string): string {
  const parts = [`${fmtInt(d.streamsTotal)} ${d.streamsTotal === 1 ? "stream" : "streams"}`, `${fmtInt(d.servers.length)} ${d.servers.length === 1 ? "store" : "stores"}`];
  if (!hasRate(d)) parts.push(wait ? `no growth rate yet: ${wait}` : "no growth rate yet (6 hours of history needed)");
  else if (d.growthFrom !== undefined) parts.push(`growth from ${isoMinute(d.growthFrom)} UTC`);
  return parts.join(" · ");
}

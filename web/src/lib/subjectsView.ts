// The Subjects page in words: the filters, a row's subscribers and note,
// the rates, the footnote and the tester's answer grouped by account.
import type { CoreMatch, MatchAccount, StreamMatch, SubjectFilter, SubjectRow, Subjects } from "../api/subjects";
import { fmtInt, hms } from "./format";

export const SUBJECTS_SUBTITLE = "Where messages can go: stream subjects and client subscriptions, by subject pattern.";

export const SUBJECTS_FOOTNOTE =
  "NATS servers do not count messages per subject, so rates come from streams (sequence growth) and are shown only where a stream stores the subject.";

/** The footnote's second sentence, shown only when a subscription collection was read. */
export const SUBSCRIPTIONS_FOOTNOTE = "Subscriptions come from each server's subscription list, refreshed every few minutes rather than every collection to keep large systems cheap.";

export const NOTHING_REACHED = "no stream stores it and no subscriber listens";

export const NOTHING_NOT_COLLECTED = "no stream stores it; subscriptions were not collected, so subscribers are not known";

/** The pills in board order, with the ?filter= value each writes. */
export const FILTERS: { id: SubjectFilter; label: string; param: string }[] = [
  { id: "all", label: "All patterns", param: "" },
  { id: "stored", label: "Captured by a stream", param: "stored" },
  { id: "noConsumer", label: "Stream without a consumer", param: "no-consumer" },
  { id: "overlapping", label: "Overlapping streams", param: "overlapping" },
  { id: "wide", label: "Wide subscriptions (>)", param: "wide" },
];

/** The filter a ?filter= value names; anything unknown lists every row. */
export function subjectFilter(param: string | null): SubjectFilter {
  return FILTERS.find((f) => f.param !== "" && f.param === param)?.id ?? "all";
}

function plural(n: number, one: string, many = `${one}s`): string {
  return `${fmtInt(n)} ${n === 1 ? one : many}`;
}

/** "a, b and c". */
export function listText(items: string[]): string {
  if (items.length <= 1) return items.join("");
  return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

export function isStored(r: Pick<SubjectRow, "kind">): boolean {
  return r.kind === "stream" || r.kind === "kv" || r.kind === "object";
}

/** A row's subscribers: the words before the names, the connection names and how many more there are. */
export interface Subscribers {
  lead: string;
  /** The queue group every subscription shares, after the lead; "" for none. */
  queue: string;
  names: string[];
  more: number;
  /** The lead counts the stream's consumers, so it can open them. */
  consumers: boolean;
}

function consumerWords(r: SubjectRow): string {
  const eph = r.kind === "kv" ? plural(r.ephemeral, "watcher") : `${fmtInt(r.ephemeral)} ephemeral`;
  if (r.durable > 0 && r.ephemeral > 0) return `${fmtInt(r.durable)} durable, ${eph}`;
  if (r.durable > 0) return plural(r.durable, "durable consumer");
  if (r.ephemeral > 0) return eph;
  return "none";
}

/**
 * "6 durable consumers", "1 ephemeral" with the connection that reads it,
 * "22 watchers" on a KV bucket; "3 connections in queue group
 * order-service" on a subscription; "server" on the JetStream API.
 */
export function subscribers(r: SubjectRow): Subscribers {
  if (r.kind === "jsApi") return { lead: "server", queue: "", names: [], more: 0, consumers: false };
  if (isStored(r)) {
    const lead = consumerWords(r);
    return { lead, queue: "", names: r.names, more: r.moreNames, consumers: lead !== "none" };
  }
  if (r.connections === 0) return { lead: "none", queue: "", names: [], more: 0, consumers: false };
  const lead = plural(r.connections, "connection") + (r.queue ? " in queue group" : "");
  return { lead, queue: r.queue, names: r.names, more: r.moreNames, consumers: false };
}

/** "(a, b, c +2)", or "" without names. */
export function namesText(names: string[], more: number): string {
  if (names.length === 0) return more > 0 ? `(+${fmtInt(more)})` : "";
  return `(${names.join(", ")}${more > 0 ? ` +${fmtInt(more)}` : ""})`;
}

export function subscribersText(r: SubjectRow): string {
  const s = subscribers(r);
  const names = namesText(s.names, s.more);
  const lead = s.queue ? `${s.lead} ${s.queue}` : s.lead;
  return names ? `${lead} ${names}` : lead;
}

/** A row's note: words, then stream names (shown as links), then words; warn marks the row. */
export interface Note {
  lead: string;
  streams: string[];
  warn: boolean;
}

const NO_CONSUMER: Record<string, string> = {
  interest: "messages are dropped",
  workqueue: "messages wait for a consumer",
  limits: "messages are kept but nobody reads them",
};

/** "3.1", "42"; under 10 with one decimal. */
function perSecond(v: number): string {
  return v < 10 ? String(Math.round(v * 10) / 10) : fmtInt(Math.round(v));
}

/** "0.2%", "<0.1%", "0%". */
export function shareText(share: number): string {
  const pct = share * 100;
  if (pct === 0) return "0%";
  if (pct < 0.1) return "<0.1%";
  return `${pct < 10 ? Math.round(pct * 10) / 10 : Math.round(pct)}%`;
}

function apiNote(r: SubjectRow): string {
  if (r.apiPerSecond === undefined) return "JetStream API";
  const errors = r.apiErrorShare === undefined ? "" : `, ${shareText(r.apiErrorShare)} errors`;
  return `JetStream API: ${perSecond(r.apiPerSecond)} req/s${errors}`;
}

/** The first fact of the row that applies, in the order of the plan; undefined for none. */
export function note(r: SubjectRow): Note | undefined {
  const words = (lead: string, warn = false): Note => ({ lead, streams: [], warn });
  if (r.overlaps.length > 0) return { lead: "overlaps", streams: r.overlaps, warn: true };
  if (r.noConsumer) {
    const why = NO_CONSUMER[r.retention];
    return words(why ? `stream without a consumer: ${why}` : "stream without a consumer", true);
  }
  if (isStored(r)) {
    if (r.retention === "workqueue" && r.messages === 0) return words("work queue, empty");
    if (r.retention === "interest") return words("interest retention: kept only while someone listens");
    if (r.mirrorOf) return { lead: "mirror of", streams: [r.mirrorOf], warn: false };
    if (r.sourcedInto.length > 0) return { lead: "sourced into", streams: r.sourcedInto, warn: false };
    if (r.mirroredBy.length > 0) return { lead: "mirrored by", streams: r.mirroredBy, warn: false };
    return undefined;
  }
  if (r.kind === "subscription" && r.patterns.length === 1 && r.patterns[0] === ">") return words("receives every message in the account");
  if (r.kind === "inbox") return words("reply inboxes");
  if (r.kind === "jsApi") return words(apiNote(r));
  return undefined;
}

export function noteText(r: SubjectRow): string {
  const n = note(r);
  if (!n) return "";
  return n.streams.length ? `${n.lead} ${n.streams.join(", ")}` : n.lead;
}

/** Stored messages per second: "0", "<0.01", "0.08", "14.9", "1,204"; undefined when not known. */
export function rateText(v: number | undefined): string | undefined {
  if (v === undefined) return undefined;
  if (v === 0) return "0";
  if (v < 0.01) return "<0.01";
  if (v < 10) return v.toFixed(2);
  if (v < 100) return v.toFixed(1);
  return fmtInt(Math.round(v));
}

/** The footnote's lines after the board's text: the collection's time, unanswered servers, cut lists and what was left out. */
export function footLines(d: Subjects): string[] {
  const lines: string[] = [];
  if (d.subscriptionsAt === undefined) {
    // The notice above the table says so.
    return lines;
  }
  lines.push(`Subscriptions as of ${hms(d.subscriptionsAt)} UTC.`);
  const silent = d.servers.filter((s) => !s.answered).map((s) => s.name);
  if (silent.length) lines.push(`${listText(silent)} did not answer for ${silent.length === 1 ? "its" : "their"} subscription list; ${silent.length === 1 ? "its" : "their"} subscriptions are missing.`);
  const cut = d.servers.filter((s) => s.answered && s.total !== undefined && s.listed !== undefined && s.listed < s.total);
  if (cut.length) lines.push(`Lists cut at the limit: ${cut.map((s) => `${s.name} listed ${fmtInt(s.listed!)} of ${fmtInt(s.total!)}`).join("; ")}.`);
  if (d.serverSubscriptions > 0) lines.push(`${plural(d.serverSubscriptions, "subscription")} of the servers' own clients (JetStream, system and account internals) left out.`);
  if (d.unlistedSubscriptions > 0)
    lines.push(`${plural(d.unlistedSubscriptions, "subscription")} left out whose connection the server's connection list did not include, so it cannot be told from the server's own.`);
  return lines;
}

/** "The first 1,000 of 1,204 rows are listed.", or "" when every row is. */
export function cutText(d: Pick<Subjects, "rows" | "rowsTotal">): string {
  return d.rowsTotal > d.rows.length ? `The first ${fmtInt(d.rows.length)} of ${fmtInt(d.rowsTotal)} rows are listed.` : "";
}

/** The server's reason a subject was refused, without the code's prefix. */
export function invalidReason(message: string): string {
  return message.replace(/^invalid[ _]argument:\s*/i, "");
}

export const JETSTREAM_API_ANSWERS = "the account's JetStream API answers it";

/** Whether a message published in the account reaches nothing. */
export function reachesNothing(a: Pick<MatchAccount, "streams" | "core" | "jetstreamApi">): boolean {
  return a.streams.length === 0 && a.core.length === 0 && !a.jetstreamApi;
}

/** "2 ephemeral", or on a KV bucket "22 watchers"; "" for none. */
function ephemeralText(s: StreamMatch): string {
  if (s.ephemeral === 0) return "";
  return s.kind === "kv" ? plural(s.ephemeral, "watcher") : `${fmtInt(s.ephemeral)} ephemeral`;
}

/** "analytics, email-sender and 2 ephemeral", "22 watchers" on a KV bucket, or "" for none. */
export function consumersText(s: StreamMatch): string {
  const parts = [...s.consumers];
  const eph = ephemeralText(s);
  if (eph) parts.push(eph);
  return listText(parts);
}

/** Who reads a matching stream: the chip's label, the ephemeral consumers in words, and whether nobody reading it is a warning. */
export interface Readers {
  label: string;
  ephemeral: string;
  warn: boolean;
}

/**
 * A stream read by consumers: "then consumers"; a KV bucket: "then read
 * by". Nobody reading warns only on a stream that no mirror or sourcing
 * stream copies, as the table's "without a consumer" does.
 */
export function readers(s: StreamMatch): Readers {
  const eph = ephemeralText(s);
  if (s.consumers.length > 0 || eph) return { label: s.kind === "kv" ? "then read by" : "then consumers", ephemeral: eph, warn: false };
  if (s.kind === "kv") return { label: "no watcher", ephemeral: "", warn: false };
  if (s.kind === "object") return { label: "no consumer", ephemeral: "", warn: false };
  const copied = s.mirrors.length > 0 || s.sourcedBy.length > 0;
  return { label: copied ? "no consumer" : "no consumer reads it", ephemeral: "", warn: !copied };
}

/** "one of 3 in queue group", "136 connections", with the queue group apart so it can stay on one line. */
export function coreParts(c: CoreMatch): { lead: string; queue: string } {
  if (c.queue) return { lead: c.connections > 1 ? `one of ${fmtInt(c.connections)} in queue group` : "1 connection in queue group", queue: c.queue };
  return { lead: plural(c.connections, "connection"), queue: "" };
}

/** "one of 3 in queue group order-service", "136 connections". */
export function coreLead(c: CoreMatch): string {
  const p = coreParts(c);
  return p.queue ? `${p.lead} ${p.queue}` : p.lead;
}

export function coreText(c: CoreMatch): string {
  const names = namesText(c.names, c.moreNames);
  return `${coreLead(c)}${names ? ` ${names}` : ""} via ${c.subject}`;
}

// The Accounts page's wording, order and colours, apart from React so they
// can be tested on their own.
import type { AccountRow, AccountsAndAuth, Expiring, FailedLogin } from "../api/accounts";
import { dayMonHm, fmtBytes, fmtDuration, fmtInt, hm, isoDate } from "./format";

const DAY = 86_400_000;
const HOUR = 3_600_000;

/** A bar fills to the warning colour at this share of its limit. */
export const LIMIT_WARN = 0.8;

/** A failed-login group averaging this many attempts an hour over the window is in the warning colour. */
export const HOT_PER_HOUR = 60;

/** What expires within this time of the collection is listed, in the warning colour. */
export const EXPIRING_WINDOW_MS = 30 * DAY;

function plural(n: number, one: string, many = `${one}s`): string {
  return `${fmtInt(n)} ${n === 1 ? one : many}`;
}

/** "Operator mode · 3 accounts · names from the account JWTs" */
export function headerLine(d: AccountsAndAuth): string {
  const mode = d.operatorMode ? "Operator mode" : "Config mode";
  const from = d.operatorMode ? "names from the account JWTs" : "names from the server configuration";
  return `${mode} · ${plural(d.accountsTotal, "account")} · ${from}`;
}

/** An account's readable name, else its identifier. */
export function accountTitle(a: { account: string; accountName: string }): string {
  return a.accountName || a.account;
}

/** Whether an identifier is an account public key: 56 characters starting with A, the collector's rule. */
export function isAccountKey(id: string): boolean {
  return id.length === 56 && id.startsWith("A");
}

/**
 * The identifier under an account's name: an account key shortened to
 * "ADSU…NH7Q2", any other identifier as it is; undefined when it is the
 * name itself, as in config mode.
 */
export function shortIdentifier(a: { account: string; accountName: string }): string | undefined {
  if (!a.accountName || a.accountName === a.account) return undefined;
  return isAccountKey(a.account) ? `${a.account.slice(0, 4)}…${a.account.slice(-5)}` : a.account;
}

/** Code unit order, as the server compares names. */
function cmpStr(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

/** The server's order, kept on the page: the system account last, the others by connections, most first, then by name. */
export function compareAccounts(a: AccountRow, b: AccountRow): number {
  if (a.isSystem !== b.isSystem) return a.isSystem ? 1 : -1;
  const c = (b.connections ?? 0) - (a.connections ?? 0);
  if (c !== 0) return c;
  return cmpStr(accountTitle(a), accountTitle(b)) || cmpStr(a.account, b.account);
}

export function orderAccounts(rows: AccountRow[]): AccountRow[] {
  return [...rows].sort(compareAccounts);
}

/** No connections, no leaf nodes and no streams in the collection; unknown counts are not idle. */
export function isIdle(a: AccountRow): boolean {
  return a.connections === 0 && (a.leafnodes ?? 0) === 0 && a.streams === 0;
}

export type Tone = "ok" | "warn" | "crit" | "muted";

/** A bar against a limit: only for a limit that is set and above 0. */
export interface LimitBar {
  /** The fill, 0 to 100. */
  pct: number;
  /** The real share of the limit in percent, above 100 over the limit. */
  share: number;
  /** ok below 80%, warn from 80% up to the limit, crit over it. */
  tone: "ok" | "warn" | "crit";
}

export function limitBar(used: number | undefined, limit: number | undefined): LimitBar | undefined {
  if (used === undefined || limit === undefined || limit <= 0) return undefined;
  const share = (used * 100) / limit;
  const tone = used > limit ? "crit" : share >= LIMIT_WARN * 100 ? "warn" : "ok";
  return { pct: Math.min(100, Math.max(0, share)), share, tone };
}

/** The bar's words, also its non-colour cue: "82% of the limit", "150% of the limit, over the limit". */
export function limitBarText(bar: LimitBar): string {
  return limitBarParts(bar).join(", ");
}

/** The bar's words in parts, each kept on one line: ["150% of the limit", "over the limit"]. */
export function limitBarParts(bar: LimitBar): string[] {
  const pct = `${fmtInt(Math.floor(bar.share))}% of the limit`;
  return bar.tone === "crit" ? [pct, "over the limit"] : [pct];
}

/** "of 2,000", "of unlimited", or undefined without a limit. */
export function connLimitText(limit: number | undefined): string | undefined {
  if (limit === undefined) return undefined;
  return limit < 0 ? "of unlimited" : `of ${fmtInt(limit)}`;
}

/** Whether the account JWT disables JetStream: jetstream_enabled false. Unset (no known JWT) is not off. */
export function jetStreamOff(a: AccountRow): boolean {
  return a.jetstreamEnabled === false;
}

/** Whether the account JWT allows JetStream memory but no file storage: enabled with a disk limit of 0. */
export function memoryOnly(a: AccountRow): boolean {
  return a.jetstreamEnabled === true && a.diskLimit === 0;
}

/** The disk cell's word for an account without file storage: "JetStream off", "memory only", or undefined. */
export function diskStateText(a: AccountRow): string | undefined {
  if (jetStreamOff(a)) return "JetStream off";
  if (memoryOnly(a)) return "memory only";
  return undefined;
}

/** "of 500 GiB", "of unlimited", or undefined without a limit or with a limit of 0, never "of 0 B". */
export function diskLimitText(a: AccountRow): string | undefined {
  if (a.diskLimit === undefined || a.diskLimit === 0) return undefined;
  return a.diskLimit < 0 ? "of unlimited" : `of ${fmtBytes(a.diskLimit)}`;
}

/**
 * "2 imports · 1 export", "none", "system account", "idle now", "1 export ·
 * idle now", "not looked up yet"; undefined when the account was looked up
 * and neither count is known.
 */
export function importsExports(a: AccountRow): string | undefined {
  if (a.isSystem) return "system account";
  const parts: string[] = [];
  if (!a.detailsKnown) parts.push("not looked up yet");
  else {
    if ((a.imports ?? 0) > 0) parts.push(plural(a.imports!, "import"));
    if ((a.exports ?? 0) > 0) parts.push(plural(a.exports!, "export"));
    if (parts.length === 0 && a.imports === 0 && a.exports === 0 && !isIdle(a)) parts.push("none");
  }
  if (isIdle(a)) parts.push("idle now");
  return parts.length > 0 ? parts.join(" · ") : undefined;
}

/** The JWT expires cell: its text, a tone, and the reason a dash stands for. */
export interface JwtCell {
  text?: string;
  tone?: Tone;
  /** For screen readers when text is undefined and the cell shows a dash. */
  why?: string;
  /** Under a date within 30 days, the cue that does not rest on colour: "in 12 days". */
  note?: string;
}

export function jwtCell(a: AccountRow, operatorMode: boolean, epochAt: number | undefined): JwtCell {
  if (!operatorMode) return { why: "no account JWTs in config mode" };
  // Imports / exports says it visibly; one row says it once.
  if (!a.detailsKnown) return { why: "not looked up yet" };
  if (!a.hasJwt) return { why: "no account JWT" };
  if (a.expired) return { text: "expired", tone: "crit" };
  if (a.jwtExpires === undefined) return { text: "never", tone: "muted" };
  if (epochAt !== undefined && a.jwtExpires <= epochAt) return { text: "expired", tone: "crit" };
  if (epochAt === undefined || a.jwtExpires - epochAt > EXPIRING_WINDOW_MS) return { text: isoDate(a.jwtExpires) };
  return { text: isoDate(a.jwtExpires), tone: "warn", note: `in ${expiresIn({ expires: a.jwtExpires, expired: false }, epochAt)}` };
}

/**
 * The window failed logins are counted over, in milliseconds: 24 hours, or
 * less when the history is shorter. A start at or after the collection (the
 * first collection) is a history of 0, not a day.
 */
export function loginWindowMs(d: Pick<AccountsAndAuth, "epochAt" | "failedLoginsWindowStart">): number {
  if (d.epochAt === undefined || d.failedLoginsWindowStart === undefined) return DAY;
  return Math.min(DAY, Math.max(0, d.epochAt - d.failedLoginsWindowStart));
}

/** Whether the window is shorter than 24 hours (by more than a minute), so the card says where the history starts. */
export function loginWindowShort(d: Pick<AccountsAndAuth, "epochAt" | "failedLoginsWindowStart">): boolean {
  return d.failedLoginsWindowStart !== undefined && loginWindowMs(d) < DAY - 60_000;
}

/**
 * The shortest window a rate is taken over: ten minutes, so a window of 0
 * (the first collection) has a rate and a handful of attempts in its first
 * minutes is not taken for a client retrying every minute.
 */
const MIN_RATE_WINDOW_MS = 10 * 60_000;

/** Attempts an hour over the window, the true rate; a window under ten minutes counts as ten minutes. */
export function loginRate(l: FailedLogin, windowMs: number): number {
  return l.count / (Math.max(windowMs, MIN_RATE_WINDOW_MS) / HOUR);
}

/** Whether a group averages 60 or more attempts an hour over the window. */
export function loginHot(l: FailedLogin, windowMs: number): boolean {
  return loginRate(l, windowMs) >= HOT_PER_HOUR;
}

/**
 * The hot row's cue that does not rest on colour: "about 60 an hour", or,
 * for a window under an hour, the count since its start: "70 since 14:00 UTC".
 */
export function loginRateText(l: FailedLogin, windowMs: number, windowStart?: number): string {
  if (windowMs < HOUR && windowStart !== undefined) return `${fmtInt(l.count)} since ${hm(windowStart)} UTC`;
  return `about ${fmtInt(loginRate(l, windowMs))} an hour`;
}

/** "(no user)" for a client that gave none. */
export function loginUser(l: FailedLogin): string {
  return l.user || "(no user)";
}

/** "The 100 most frequent of 140 groups are listed; 2,000 failed logins in all." when groups were cut. */
export function loginsCutLine(d: AccountsAndAuth): string | undefined {
  if (d.failedLoginGroupsTotal <= d.failedLogins.length) return undefined;
  return `The ${fmtInt(d.failedLogins.length)} most frequent of ${fmtInt(d.failedLoginGroupsTotal)} groups are listed; ${plural(d.failedLoginsTotal, "failed login")} in all.`;
}

const LISTENER: Record<string, string> = {
  client: "client",
  cluster: "cluster",
  gateway: "gateway",
  leafnode: "leaf node",
  websocket: "websocket",
  mqtt: "MQTT",
};

/** "client listener", "leaf node listener"; an unknown name as it is. */
export function listenerLabel(listener: string): string {
  return `${LISTENER[listener] ?? listener} listener`;
}

/** The kind of thing that expires, after its name: "certificate, client listener", "account JWT", "user JWT". */
export function expiringWhat(e: Expiring): string {
  switch (e.kind) {
    case "certificate":
      return `certificate, ${listenerLabel(e.listener)}`;
    case "accountJwt":
      return "account JWT";
    case "userJwt":
      return "user JWT";
    default:
      return "";
  }
}

/**
 * A certificate read from an older collection (its server has not answered
 * since): "read at 13:20 UTC; no answer from the server since", with the day when the UTC date
 * differs from the collection's; undefined for a row read from the
 * collection itself.
 */
export function seenAtText(e: Expiring, epochAt: number | undefined): string | undefined {
  return seenAtParts(e, epochAt)?.join("; ");
}

/** seenAtText in parts, each kept on one line: ["read at 13:20 UTC", "no answer from the server since"]. */
export function seenAtParts(e: Expiring, epochAt: number | undefined): string[] | undefined {
  if (e.seenAt === undefined) return undefined;
  const when = epochAt !== undefined && isoDate(epochAt) === isoDate(e.seenAt) ? hm(e.seenAt) : dayMonHm(e.seenAt);
  return [`read at ${when} UTC`, "no answer from the server since"];
}

/** The failed-logins card when no event was stored up to the collection, in the words of Replay's events-not-captured reason; past tense, so it holds on a past range. */
export const EVENTS_NOT_CAPTURED_TEXT =
  "Failed logins are not shown: no event was stored up to this collection, so event capture was not running for this system then.";

/** "16 days", "1 day", "5h 12m", or "expired". */
export function expiresIn(e: Pick<Expiring, "expires" | "expired">, epochAt: number | undefined): string {
  if (e.expired || e.expires === undefined) return "expired";
  if (epochAt === undefined) return isoDate(e.expires);
  const left = e.expires - epochAt;
  if (left <= 0) return "expired";
  if (left >= DAY) return plural(Math.floor(left / DAY), "day");
  return fmtDuration(left);
}

/** Expired rows are critical; every other listed row expires within 30 days and is a warning. */
export function expiringTone(e: Expiring, epochAt: number | undefined): Tone {
  if (e.expired || e.expires === undefined || (epochAt !== undefined && e.expires <= epochAt)) return "crit";
  return "warn";
}

/** "4 more expire later; 9 never expire." with the cut when rows were cut; undefined when there is nothing to count. */
export function expiringCountLine(d: AccountsAndAuth): string | undefined {
  const parts: string[] = [];
  if (d.expiringTotal > d.expiring.length) parts.push(`the ${fmtInt(d.expiring.length)} soonest of ${fmtInt(d.expiringTotal)} are listed`);
  const more = d.expiringTotal > 0 ? "more " : "";
  if (d.expireLater > 0) parts.push(`${fmtInt(d.expireLater)} ${more}${d.expireLater === 1 ? "expires" : "expire"} later`);
  if (d.neverExpire > 0) parts.push(`${fmtInt(d.neverExpire)} never ${d.neverExpire === 1 ? "expires" : "expire"}`);
  if (parts.length === 0) return undefined;
  const s = parts.join("; ");
  return `${s[0].toUpperCase()}${s.slice(1)}.`;
}

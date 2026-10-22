// The Micro services page in words: the line under the title, the cards'
// numbers and notes, the endpoint table's cells, the instances, the
// accounts that were asked and did not answer, and the footnote.
import type { MicroAccount, MicroEndpoint, MicroInstanceSource, MicroMissing, MicroRequest, MicroService, MicroServices } from "../api/micro";
import { dayMonHm, fmtInt, hm, hms } from "./format";
import { listText } from "./subjectsView";

/** The line under the title, in three parts so the subjects can be set in mono. */
export const MICRO_SUBTITLE = ["Services built on the NATS micro framework, found through", "$SRV.INFO", "and measured through", "$SRV.STATS", "."] as const;

export const MICRO_SUBTITLE_TEXT = `${MICRO_SUBTITLE[0]} ${MICRO_SUBTITLE[1]} ${MICRO_SUBTITLE[2]} ${MICRO_SUBTITLE[3]}${MICRO_SUBTITLE[4]}`;

/** A configuration key in a sentence, set in mono; never read from text. */
export interface Key {
  key: string;
}

/** A sentence in parts: text as it is, and keys. */
export type Phrase = readonly (string | Key)[];

const ACCOUNTS: Key = { key: "accounts" };

/** A phrase as one string, each key in backticks; for tests and plain text. */
export function plain(p: Phrase): string {
  return p.map((x) => (typeof x === "string" ? x : `\`${x.key}\``)).join("");
}

export const MICRO_FOOTNOTE: Phrase = [
  "Each account listed under ",
  ACCOUNTS,
  " gets one $SRV.STATS request every collection and a $SRV.INFO request every few minutes; rates, times and errors cover the 15 minutes before the range's end. An endpoint is slow when its average time is at least 3 times its 7-day level and at least 5 ms above it. A service instance that stops answering shows as “instance missing”, a version spread across instances as “mixed versions”.",
];

/** What the account's user needs for nats-lens to ask its services. */
export const GRANTS = "publish on $SRV.INFO and $SRV.STATS and subscribe on _INBOX.>";

/** The grants as a clause; the card's note, the endpoint section and the notice all say it so. */
const NEEDS = `the account's user needs to ${GRANTS}`;

/** The notice above the services when no subscription list was read. */
export const NO_SUBSCRIPTIONS: Phrase = [
  "No subscription list within 24 hours before this collection: only the services of accounts listed under ",
  ACCOUNTS,
  " are shown, and instances name no connection.",
];

const DAY = 86_400_000;

function plural(n: number, one: string, many = `${one}s`): string {
  return `${fmtInt(n)} ${n === 1 ? one : many}`;
}

/** 08:12 within a day before ref, else 20 Oct 01:16. */
export function whenText(at: number, ref: number): string {
  return ref - at < DAY ? hm(at) : dayMonHm(at);
}

const round1 = (v: number) => Math.round(v * 10) / 10;

/** Requests per second, rounded before the format is chosen: "161.7", "88.0", "<0.1", "0", "1,000". */
export function rateText(v: number): string {
  if (v === 0) return "0";
  if (v < 0.1) return "<0.1";
  const r = round1(v);
  return r < 1000 ? r.toFixed(1) : fmtInt(Math.round(v));
}

/** An average time, rounded before the unit is chosen: "3.8 ms", "10 ms", "<0.1 ms", "1.0 s", "12 s". */
export function msText(v: number): string {
  if (v === 0) return "0 ms";
  if (v < 0.1) return "<0.1 ms";
  if (round1(v) < 10) return `${round1(v).toFixed(1)} ms`;
  if (Math.round(v) < 1000) return `${Math.round(v)} ms`;
  const s = round1(v / 1000);
  return s < 10 ? `${s.toFixed(1)} s` : `${fmtInt(Math.round(v / 1000))} s`;
}

/**
 * An error share: "0%", "<0.1%", "0.4%", "2.4%", "12%". high is the
 * server's above-1% flag; a flagged share that rounds to 1.0% reads
 * ">1.0%", so the words never contradict the colour.
 */
export function errorsText(share: number, high: boolean): string {
  const pct = share * 100;
  if (pct === 0) return "0%";
  if (pct < 0.1) return "<0.1%";
  const r = round1(pct);
  if (high && r <= 1) return ">1.0%";
  return r < 10 ? `${r.toFixed(1)}%` : `${Math.round(pct)}%`;
}

/** "1.4.2", "2.0.0 / 1.9.3", "3.0 / 2.9 / 2.8 +1"; "" when no version is known. */
export function versionText(s: Pick<MicroService, "versions">): string {
  const shown = s.versions.slice(0, 3).map((v) => v.version || "no version");
  const more = s.versions.length - shown.length;
  return shown.join(" / ") + (more > 0 ? ` +${fmtInt(more)}` : "");
}

/** The address's ?service= value of a service. */
export function serviceKey(s: Pick<MicroService, "account" | "name">): string {
  return `${s.account}/${s.name}`;
}

/**
 * ?service= in its parts; an empty or missing account part is "". It is
 * split at the last slash: the micro framework allows only letters,
 * digits, "-" and "_" in a service name, while an account's name may
 * hold a slash.
 */
function serviceParam(param: string): { account: string; name: string } {
  const cut = param.lastIndexOf("/");
  return cut < 0 ? { account: "", name: param } : { account: param.slice(0, cut), name: param.slice(cut + 1) };
}

/**
 * The service ?service= names, else the first. A name without an account
 * part is looked for in the scoped account, or by name without a scope.
 * unknown: the parameter names a service the list does not hold.
 */
export function chooseService(services: MicroService[], param: string | null, scope = ""): { chosen?: MicroService; unknown: boolean } {
  const p = param ? serviceParam(param) : undefined;
  const named = !p ? undefined : p.account ? services.find((s) => s.account === p.account && s.name === p.name) : services.find((s) => s.name === p.name && (!scope || s.account === scope));
  return { chosen: named ?? services[0], unknown: !!param && !named };
}

/**
 * What the page says when ?service= names a service the list does not
 * hold: that it belongs to another account than the scoped one, or that
 * this collection has none of that name.
 */
export function unknownServiceText(param: string, scope: string, label: (id: string) => string, shown: string): string {
  const p = serviceParam(param);
  // No account part: the scoped account's.
  const account = p.account || scope;
  const name = p.name;
  if (!name) return `No service named in the address is in this collection; showing ${shown}.`;
  if (!account) return `No service ${name} in this collection; showing ${shown}.`;
  if (scope && account !== scope) return `${name} of ${label(account)} is not in account ${label(scope)}; showing ${shown}.`;
  if (scope) return `No service ${name} in ${label(scope)} in this collection; showing ${shown}.`;
  return `No service ${param} in this collection; showing ${shown}.`;
}

/**
 * The response with each empty account name filled from names (account
 * to name, as the header's account list has them); the response itself
 * when nothing is filled.
 */
export function withAccountNames(d: MicroServices, names: Map<string, string>): MicroServices {
  const fill = <T extends { account: string; accountName: string }>(x: T): T => (!x.accountName && names.get(x.account) ? { ...x, accountName: names.get(x.account)! } : x);
  if (!d.services.some((s) => fill(s) !== s) && !d.accounts.some((a) => fill(a) !== a)) return d;
  return { ...d, services: d.services.map(fill), accounts: d.accounts.map(fill) };
}

/** "Order intake and lookup", or what the card knows for a service seen only. */
export function descriptionText(s: Pick<MicroService, "asked" | "description">): string {
  if (!s.asked) return "seen in subscriptions only";
  return s.description;
}

/** Whether the service's errors are above 1%: the card shows them in the critical colour. */
export function serviceErrorsHigh(s: Pick<MicroService, "errorShare">): boolean {
  return s.errorShare !== undefined && s.errorShare > 0.01;
}

function permissionRefused(r: MicroRequest): boolean {
  return r.state === "error" && /permissions? violation/i.test(r.error);
}

/** Why a service's instances gave no answer to $SRV.STATS, from its account's request. */
export function noAnswerReason(stats: MicroRequest | undefined): string {
  if (!stats) return "its instances did not reply";
  switch (stats.state) {
    case "error":
      return permissionRefused(stats) ? `refused; ${NEEDS}` : `the request failed: ${stats.error}`;
    case "timeout":
      return "no reply within the collect window";
    case "noResponders":
      return "no responders in the account";
    case "notAsked":
      return "not asked in this collection";
    default:
      return "its instances did not reply";
  }
}

export interface CardNote {
  text: Phrase;
  warn: boolean;
}

/** The card's note, worded from the note the server chose. */
export function noteOf(s: MicroService, accounts: MicroAccount[], ref: number): CardNote {
  const n = noteWords(s, accounts, ref);
  return { text: typeof n.text === "string" ? [n.text] : n.text, warn: n.warn };
}

function noteWords(s: MicroService, accounts: MicroAccount[], ref: number): { text: string | Phrase; warn: boolean } {
  switch (s.note) {
    case "missing": {
      const since = s.missing.map((m) => m.since).filter((t): t is number => t !== undefined);
      const at = since.length ? ` since ${whenText(Math.min(...since), ref)}` : "";
      if (s.missing.length === 0) return { text: "an instance missing", warn: true };
      if (s.missing.length === 1) return { text: `instance missing: ${s.missing[0].id}${at}`, warn: true };
      return { text: `${plural(s.missing.length, "instance")} missing${at}`, warn: true };
    }
    case "mixedVersions":
      return { text: `mixed versions: ${s.versions.map((v) => `${fmtInt(v.instances)} on ${v.version || "no version"}`).join(", ")}`, warn: true };
    case "slow": {
      const e = s.endpoints.find((x) => x.slow);
      const since = e?.slowSince !== undefined ? ` since ${whenText(e.slowSince, ref)}` : "";
      return { text: `${e ? `${s.name}.${e.name}` : "an endpoint"} slower than usual${since}`, warn: true };
    }
    case "errors": {
      const e = s.endpoints.find((x) => x.errorsHigh);
      if (!e || e.errorShare === undefined) return { text: "errors above 1% on an endpoint", warn: true };
      return { text: `errors ${errorsText(e.errorShare, true)} on ${s.name}.${e.name}`, warn: true };
    }
    case "noAnswer": {
      const a = accounts.find((x) => x.account === s.account);
      return { text: `no answer to $SRV.STATS: ${noAnswerReason(a?.stats)}`, warn: true };
    }
    case "notAsked":
      return { text: ["list the account under ", ACCOUNTS, " for versions, endpoints and timings"], warn: false };
    default:
      return { text: "healthy", warn: false };
  }
}

/** "avg time 10× its 7-day level since 08:12". */
export function slowText(e: Pick<MicroEndpoint, "averageMs" | "levelMs" | "slowSince">, ref: number): string {
  const since = e.slowSince !== undefined ? ` since ${whenText(e.slowSince, ref)}` : "";
  if (e.averageMs === undefined || e.levelMs === undefined || e.levelMs <= 0) return `avg time above its 7-day level${since}`;
  const r = e.averageMs / e.levelMs;
  return `avg time ${r >= 10 ? fmtInt(Math.round(r)) : String(Math.round(r * 10) / 10)}× its 7-day level${since}`;
}

/** "missing since 10:05, last answered 10:04; still subscribed, so registered but not answering". */
export function missingText(m: MicroMissing, asked: boolean, ref: number): string {
  const since = m.since !== undefined ? whenText(m.since, ref) : undefined;
  const parts = [since !== undefined ? `missing since ${since}` : "missing"];
  // The same minute twice says nothing more.
  if (m.lastSeen !== undefined && whenText(m.lastSeen, ref) !== since) parts.push(`${asked ? "last answered" : "last seen"} ${whenText(m.lastSeen, ref)}`);
  let t = parts.join(", ");
  if (m.stillSubscribed) t += "; still subscribed, so registered but not answering";
  return t;
}

/** What the endpoint section says when it has no rows. */
export function noEndpointsText(s: MicroService, accounts: MicroAccount[]): string {
  if (!s.asked) return "";
  if (!s.answered) {
    const a = accounts.find((x) => x.account === s.account);
    return `No endpoints known: none of its instances answered $SRV.STATS in this collection (${noAnswerReason(a?.stats)}).`;
  }
  return "The service reports no endpoints.";
}

/** For a service seen only: what is known and how to see more. */
export function seenOnlyText(s: MicroService, accountLabel: string): Phrase[] {
  const conns = s.connectionNames.length ? ` on ${listText(s.connectionNames)}${s.moreNames > 0 ? ` and ${plural(s.moreNames, "more connection")}` : ""}` : "";
  return [
    [`nats-lens does not ask ${accountLabel}, so versions, endpoints and timings are not known. The subscription list shows ${plural(s.instances, "instance")}${conns}.`],
    [`To see more, list ${accountLabel} under `, ACCOUNTS, ` in the configuration, with a user that may ${GRANTS}.`],
  ];
}

/** Why an asked account with no service listed did not answer; undefined when it answered, had no responders or was not asked. */
export function unansweredText(a: MicroAccount): string | undefined {
  if (!a.asked || a.services > 0) return undefined;
  const r = a.stats;
  if (r.state === "error") {
    if (permissionRefused(r)) return `The server refused $SRV.STATS (${r.error}): ${NEEDS}.`;
    return `$SRV.STATS failed: ${r.error}.`;
  }
  if (r.state === "timeout") return "No reply to $SRV.STATS came within the collect window.";
  return undefined;
}

/** "The first 200 of 312 services are listed.", or "". */
export function cutText(shown: number, total: number, what: string): string {
  return total > shown ? `The first ${fmtInt(shown)} of ${fmtInt(total)} ${what} are listed.` : "";
}

/** The footnote's lines after its text: the subscription list's time, accounts without services, cut lists. */
export function footLines(d: MicroServices, label: (a: MicroAccount) => string): string[] {
  const lines: string[] = [];
  if (d.subscriptionsAt !== undefined) lines.push(`Subscriptions as of ${hms(d.subscriptionsAt)} UTC.`);
  const quiet = d.accounts.filter((a) => a.asked && a.services === 0 && a.stats.state === "noResponders").map(label);
  if (quiet.length) lines.push(`Asked, with no service answering (no responders): ${listText(quiet)}.`);
  const cut = cutText(d.services.length, d.servicesTotal, "services");
  if (cut) lines.push(cut);
  return lines;
}

/** What the page says with no service. */
export function noServiceText(d: MicroServices, scopeLabel: string): Phrase {
  const where = scopeLabel ? ` in ${scopeLabel}` : "";
  if (d.subscriptionsAt === undefined && !d.accounts.some((a) => a.asked))
    return [`No micro service known${where}: no account is listed under `, ACCOUNTS, ", and no subscription list within 24 hours before this collection."];
  return [`No micro service${where} in this collection.`];
}

/** The heading over a service's instances, by where they come from. */
export function instancesHeading(source: MicroInstanceSource): string {
  if (source === "subscriptions") return "Instances seen in the subscription list";
  if (source === "lastAnswered") return "Instances that last answered within the hour before this collection";
  return "Instances";
}

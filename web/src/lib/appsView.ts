// The Applications page in words: the line under the title, the three
// summary cards, the table's cells and sort values, the sparkline's
// accessible name, the notices and the footnote.
import type { AppHour, AppLibrary, Application, Applications, AppSpread, AppsLastDay, LibraryUse } from "../api/applications";
import type { SortValue } from "./sort";
import { fmtBytes, fmtInt, hm, hms } from "./format";

/** The name the server gives connections without an application. */
export const UNNAMED = "(unnamed)";

/** The tooling toggle's address parameter, as on the Connections page. */
export const TOOLING_PARAM = "tooling";

function plural(n: number, one: string, many = `${one}s`): string {
  return `${fmtInt(n)} ${n === 1 ? one : many}`;
}

/** "187 connections, 10 applications", with the limit when the list was cut. */
export function countsText(d: Pick<Applications, "connectionsTotal" | "applicationsTotal" | "applications">): string {
  const base = `${plural(d.connectionsTotal, "connection")}, ${plural(d.applicationsTotal, "application")}`;
  return d.applications.length < d.applicationsTotal ? `${base}; the first ${fmtInt(d.applications.length)} listed` : base;
}

/** "nats.js 3.0.2"; "go" without a version; "library not reported" without either. */
export function libraryName(l: Pick<AppLibrary, "lang" | "version">): string {
  if (!l.lang && !l.version) return "library not reported";
  return [l.lang || "unknown library", l.version].filter(Boolean).join(" ");
}

/**
 * The Spread card. A concentrated spread says what a restart does; any
 * other says which server serves the most.
 */
export function spreadText(s: AppSpread): string {
  if (s.total === 0 || !s.server) return "No connection to spread.";
  if (s.servers === 1) return `Every connection is on ${s.server}: a restart of it moves all ${plural(s.total, "connection")} at once.`;
  const serves = `${s.server} serves ${fmtInt(s.connections)} of ${plural(s.total, "connection")} on ${fmtInt(s.servers)} servers.`;
  return s.concentrated ? `${serves} A restart of ${s.server} moves ${fmtInt(s.connections)} of ${fmtInt(s.total)} connections at once.` : `${serves} No server holds more than half.`;
}

/** The Client libraries card: "nats.js 3.0.2 in 6 applications, go 1.54.0 in 4". */
export function librariesText(libs: LibraryUse[], max = 4): string {
  if (libs.length === 0) return "No library reported.";
  const shown = libs.slice(0, max).map((l, i) => `${libraryName(l)} in ${i === 0 ? plural(l.applications, "application") : fmtInt(l.applications)}`);
  const more = libs.length - shown.length;
  return shown.join(", ") + (more > 0 ? `, and ${plural(more, "other version")}` : "") + ".";
}

/** One figure of the Last 24 h card: its number, or undefined when no event was captured. */
export interface DayFigure {
  value?: number;
  label: string;
}

export function lastDayFigures(d: AppsLastDay): DayFigure[] {
  return [
    { value: d.slowConsumers, label: d.slowConsumers === 1 ? "slow consumer" : "slow consumers" },
    { value: d.stale, label: d.stale === 1 ? "stale connection" : "stale connections" },
    { value: d.reconnecting, label: d.reconnecting === 1 ? "application reconnecting" : "applications reconnecting" },
  ];
}

/** The Library cell: one version, or "mixed" over the versions. */
export function libraryCell(a: Pick<Application, "libraries">): { text: string; mixed: boolean } {
  if (a.libraries.length === 0) return { text: "", mixed: false };
  if (a.libraries.length === 1) return { text: libraryName(a.libraries[0]), mixed: false };
  return { text: "mixed", mixed: true };
}

/** "nats.js 3.0.2 on 120, nats.js 2.29.3 on 24": the versions of a mixed cell. */
export function mixedText(libs: AppLibrary[]): string {
  return libs.map((l) => `${libraryName(l)} on ${fmtInt(l.connections)}`).join(", ");
}

/** The sub-line under an application's name: its connections, and what instances count. */
export function subLine(a: Pick<Application, "connections" | "podsTotal" | "instances">): string {
  const conns = plural(a.connections, "connection");
  if (a.podsTotal === 0) return conns;
  return `${conns}, ${plural(a.podsTotal, "pod")}`;
}

/** The pod names, or the client addresses where no name gives a pod. */
export function placesOf(a: Pick<Application, "pods" | "podsTotal" | "addresses" | "addressesTotal">): { kind: "pods" | "addresses"; items: string[]; total: number } {
  return a.podsTotal > 0 ? { kind: "pods", items: a.pods, total: a.podsTotal } : { kind: "addresses", items: a.addresses, total: a.addressesTotal };
}

/** The client-004 finding a reconnect count links to: the first unmuted one, else the first. */
export function reconnectFinding(a: Pick<Application, "findings">) {
  const all = a.findings.filter((f) => f.checkCode === "client-004");
  return all.find((f) => !f.muted) ?? all[0];
}

/**
 * The Events page link for an application's reconnects: only where its
 * connections carry the application's own name, since the Events page
 * filters by the whole connection name.
 */
export function eventsLink(a: Pick<Application, "name" | "podsTotal">): string | undefined {
  if (a.name === UNNAMED || a.name === "" || a.podsTotal > 0) return undefined;
  return `/events?client=${encodeURIComponent(a.name)}`;
}

/** The Connections page filtered by the application's name; tooling rows include tooling there. */
export function connectionsLink(a: Pick<Application, "name" | "tooling">): string | undefined {
  if (a.name === UNNAMED || a.name === "") return undefined;
  return `/connections?name=${encodeURIComponent(a.name)}${a.tooling ? `&${TOOLING_PARAM}=1` : ""}`;
}

/** Whether a row is lifted by an open finding: one that fires and is not muted. */
export function hasOpenFinding(a: Pick<Application, "findings">): boolean {
  return a.findings.some((f) => !f.muted);
}

function sumMsgs(a: Pick<Application, "inMsgs" | "outMsgs">): number | undefined {
  if (a.inMsgs === undefined && a.outMsgs === undefined) return undefined;
  return (a.inMsgs ?? 0) + (a.outMsgs ?? 0);
}

/** The sortable columns and the value each sorts by; unset values sort last. */
export const APP_SORT: Record<string, (a: Application) => SortValue> = {
  name: (a) => a.name,
  pods: (a) => placesOf(a).total,
  instances: (a) => a.instances,
  servers: (a) => a.servers.length,
  library: (a) => (a.libraries.length > 1 ? "mixed" : a.libraries[0] ? libraryName(a.libraries[0]) : undefined),
  rtt: (a) => a.rttMs,
  subs: (a) => a.subscriptions,
  pending: (a) => a.pendingBytes,
  reconnects: (a) => a.reconnects,
  traffic: (a) => sumMsgs(a),
};

/** Hours of the 24 with a measurement. */
export function hoursSet(t: AppHour[]): number {
  return t.filter((h) => h.inMsgs !== undefined || h.outMsgs !== undefined).length;
}

/**
 * The sparkline's accessible name: what it shows, the hours, the totals,
 * and how many hours were measured when not all.
 */
export function trafficLabel(a: Pick<Application, "name" | "traffic" | "inMsgs" | "outMsgs" | "inBytes" | "outBytes">): string {
  const what = `${a.name} messages in and out`;
  if (a.traffic.length === 0) return `${what}: no hours`;
  const end = a.traffic[a.traffic.length - 1].hour + 3_600_000;
  const span = `the ${a.traffic.length} hours to ${hm(end)} UTC`;
  const set = hoursSet(a.traffic);
  if (set === 0) return `${what}, ${span}: not measured`;
  const totals = `${fmtInt(a.inMsgs ?? 0)} in (${fmtBytes(a.inBytes ?? 0)}), ${fmtInt(a.outMsgs ?? 0)} out (${fmtBytes(a.outBytes ?? 0)})`;
  const hours = set < a.traffic.length ? `; ${set} of ${a.traffic.length} hours measured` : "";
  return `${what}, ${span}: ${totals}${hours}`;
}

/**
 * The note above the table when the rollup measured fewer than the 24
 * hours for every application; undefined when some row has them all or
 * no row has traffic hours.
 */
export function trafficNote(rows: Application[]): string | undefined {
  const withHours = rows.filter((a) => a.traffic.length > 0);
  if (withHours.length === 0) return undefined;
  const len = Math.max(...withHours.map((a) => a.traffic.length));
  const covered = new Set<number>();
  for (const a of withHours) a.traffic.forEach((h, i) => (h.inMsgs !== undefined || h.outMsgs !== undefined) && covered.add(i));
  if (covered.size >= len) return undefined;
  if (covered.size === 0) return `No traffic measured in the ${len} hours before this collection's hour: the hourly rollup holds none.`;
  return `Traffic is measured for ${covered.size} of the ${len} hours before this collection's hour; the hours without a measurement are gaps in the lines, not zeros.`;
}

/** The notice for the no-event state: the 24-hour counts are not known. */
export const NO_EVENTS = "No event captured up to this collection, so reconnects, slow consumers and stale connections of the last 24 hours are not known.";

/** The notice for a cut connection list. */
export const CONNZ_CUT = "A server's connection list in this collection was cut at the collector's connz limit: its other connections are not counted here, and their traffic is missing.";

/** "1 tooling connection hidden: …", as the Connections page says it. */
export function toolingHiddenText(n: number): string {
  return `${plural(n, "tooling connection")} hidden: nats-lens's own, or clients from a loopback address.`;
}

/** The empty state: nothing listed, and why. */
export function emptyText(d: Pick<Applications, "toolingHidden">, includeTooling: boolean, scopeLabel: string): string {
  const where = scopeLabel ? ` in account ${scopeLabel}` : "";
  if (!includeTooling && d.toolingHidden > 0) return `Only tooling connections${where} in this collection: ${plural(d.toolingHidden, "connection")}. Include tooling to list them.`;
  return `No client connection${where} in this collection.`;
}

/** The line under the title. */
export function subtitleText(d: Applications | undefined): string {
  const lead = "Client connections grouped by the application in their name.";
  if (!d?.collected || d.epochAt === undefined) return `${lead} In the newest collection of the range.`;
  return `${lead} ${countsText(d)}, in the collection of ${hms(d.epochAt)} UTC.`;
}

export const APPS_FOOTNOTE = [
  "An application is the connection name before the first “@”; a connection without a name, or with nothing before the “@”, is (unnamed).",
  "Instances count pods (the part of the name after the “@”) where the name gives one, and connections otherwise. Pods lists those pod names, or the client addresses where no name gives a pod.",
  "Tooling connections, nats-lens's own or clients from a loopback address, are left out unless included.",
  "Messages in and out are the 24 whole hours before the collection's hour, from the hourly rollup; a connection that closed between two collections loses its last interval, and a gap is an hour without a measurement. Reconnects follow client-004's rule over the 24 hours before the collection.",
] as const;

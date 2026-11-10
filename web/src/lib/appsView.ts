// The Applications page in words: the line under the title, the three
// summary cards, the table's cells and sort values, the sparkline's
// accessible name, the notices and the footnote.
import type { AppHour, AppLibrary, Application, Applications, AppRunsOn, AppSpread, AppsLastDay, AppWorkload, ClientPodWatchGap, LibraryUse } from "../api/applications";
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

/** "nats.js 3.0.2"; "go" without a version; "unknown library 1.2" without a language. */
export function libraryName(l: Pick<AppLibrary, "lang" | "version">): string {
  return [l.lang || "unknown library", l.version].filter(Boolean).join(" ");
}

/**
 * The Spread card. A concentrated spread says what a restart does; any
 * other says which server serves the most. On a partial collection the
 * connections of the servers that did not answer are not known, so it
 * says what it counts and nothing of a restart.
 */
export function spreadText(s: AppSpread, partial = false): string {
  if (s.total === 0 || !s.server) return "No connection to spread.";
  if (partial) {
    if (s.servers === 1) return `Every counted connection is on ${s.server}; the servers that did not answer are not counted.`;
    return `${s.server} serves ${fmtInt(s.connections)} of ${plural(s.total, "connection")} on ${fmtInt(s.servers)} servers, counting only the servers that answered.`;
  }
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
 * The Events page link for an application's reconnects: only where every
 * connection is named exactly as the application, since the Events page
 * filters by the whole connection name.
 */
export function eventsLink(a: Pick<Application, "name" | "connectionNames" | "moreNames">): string | undefined {
  if (a.name === UNNAMED || a.name === "" || a.moreNames > 0 || a.connectionNames.length !== 1 || a.connectionNames[0] !== a.name) return undefined;
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
  runsOn: (a) => (a.runsOn?.workloads[0] ? workloadName(a.runsOn.workloads[0]) : undefined),
};

/** "shop/order-service": a workload by its namespace and name. */
export function workloadName(w: Pick<AppWorkload, "namespace" | "name">): string {
  return `${w.namespace}/${w.name}`;
}

/** The Runs on cell in words: the pods and nodes, and what was not matched. */
export function runsOnText(r: AppRunsOn, connections: number): { where?: string; coverage?: string } {
  const where = r.matched > 0 ? `${plural(r.pods, "pod")} on ${plural(r.nodesTotal, "node")}` : undefined;
  if (r.matched === connections) return { where };
  const parts: string[] = [];
  if (r.matched > 0) parts.push(`${fmtInt(r.matched)} of ${plural(connections, "connection")} matched to pods`);
  else if (r.ambiguous < connections) parts.push(connections === 1 ? "Its connection matched no pod" : "No connection matched to a pod");
  if (r.ambiguous > 0) parts.push(`${plural(r.ambiguous, "connection")} from a host-network or shared address`);
  return { where, coverage: parts.join("; ") };
}

/** The notice for client pod watches that were failing: Runs on can miss their pods. */
export function unwatchedText(gaps: ClientPodWatchGap[]): string {
  const one = (g: ClientPodWatchGap) => `${g.source}${g.since !== undefined ? ` since ${hm(g.since)} UTC` : ""}${g.error ? ` (${g.error})` : ""}`;
  return `nats-lens could not watch the ${gaps.map(one).join(", ")}: Runs on can miss the pods there.`;
}

/** The footnote's line on Runs on, shown while client pods are watched. */
export const RUNS_ON_FOOTNOTE =
  "Runs on matches each connection's client address to the watched pod that held it at the collection. A connection through a proxy or NAT, from outside the cluster or from a namespace not watched, matches no pod; one from a host-network pod, or from an address two pods held, is not matched to one pod.";

/** Hours of the 24 with a measurement. */
export function hoursSet(t: AppHour[]): number {
  return t.filter((h) => h.inMsgs !== undefined || h.outMsgs !== undefined).length;
}

/** "1,200 in (1.17 KiB)"; "in not measured" when the rollup left it unset, never 0. */
function direction(what: "in" | "out", msgs: number | undefined, bytes: number | undefined): string {
  if (msgs === undefined) return `${what} not measured`;
  return bytes === undefined ? `${fmtInt(msgs)} ${what}` : `${fmtInt(msgs)} ${what} (${fmtBytes(bytes)})`;
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
  const totals = `${direction("in", a.inMsgs, a.inBytes)}, ${direction("out", a.outMsgs, a.outBytes)}`;
  const hours = set < a.traffic.length ? `; ${set} of ${a.traffic.length} hours measured` : "";
  return `${what}, ${span}: ${totals}${hours}`;
}

/**
 * The note above the table when no row has all of its hours measured;
 * undefined when some row has them all or no row has traffic hours. It
 * reports the most hours any one row has measured.
 */
export function trafficNote(rows: Application[]): string | undefined {
  const withHours = rows.filter((a) => a.traffic.length > 0);
  if (withHours.length === 0) return undefined;
  if (withHours.some((a) => hoursSet(a.traffic) === a.traffic.length)) return undefined;
  const len = Math.max(...withHours.map((a) => a.traffic.length));
  const most = Math.max(...withHours.map((a) => hoursSet(a.traffic)));
  if (most === 0) return `No traffic measured in the ${len} hours before this collection's hour.`;
  return `No application has all ${len} hours before this collection's hour measured; the most any has is ${most}. The hours without a measurement are gaps in the lines, not zeros.`;
}

/** The notice for a partial collection: the servers whose connections it does not know; at least one. */
export function partialText(missing: string[]): string {
  const names = missing.length === 1 ? missing[0] : `${missing.slice(0, -1).join(", ")} and ${missing[missing.length - 1]}`;
  return `${names} did not answer in this collection: ${missing.length === 1 ? "its" : "their"} connections are not counted, and the counts, the spread and the traffic cover the servers that answered.`;
}

/** The notice for event capture that stopped within the 24 hours: the counts are lower bounds. */
export const CAPTURE_GAPS = "Reconnects, slow consumers and stale connections can be low: each count is at least what it says.";

/** The notice for the no-event state: the 24-hour counts are not known. */
export const NO_EVENTS = "No event captured up to this collection, so reconnects, slow consumers and stale connections of the last 24 hours are not known.";

/** The notice for a cut connection list. */
export const CONNZ_CUT = "A server's connection list in this collection was cut at the collector's connz limit: its other connections are not counted here, nor their traffic.";

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
  return `${lead} ${countsText(d)}, in the collection of ${hms(d.epochAt)} UTC${d.partial ? ", over the servers that answered" : ""}.`;
}

export const APPS_FOOTNOTE = [
  "An application is the connection name before the first “@”; a connection without a name, or with nothing before the “@”, is (unnamed).",
  "Instances count pods (the part of the name after the “@”) where the name gives one, and connections otherwise. Pods lists those pod names, or the client addresses where no name gives a pod.",
  "Tooling connections, nats-lens's own or clients from a loopback address, are left out unless included.",
  "Messages in and out are the 24 whole hours before the collection's hour, from the hourly rollup; a connection that closed between two collections loses its last interval, and a gap is an hour without a measurement.",
  "Reconnects follow client-004's rule per connection name over the 24 hours before the collection, summed over the application's names: a pod that drops and a new pod that connects, as in a rolling deploy, are not a reconnect. A name pairs its events across accounts, so a page scoped to an account counts each name with an event in that account, whatever account its other events are in.",
  "Each sparkline is scaled to its own row's busiest hour, in or out, so two rows' lines do not compare; the numbers beneath them do.",
] as const;

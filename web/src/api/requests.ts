import {eventReadId as parseEventReadId} from "./eventIdentity";
import { onCall } from "./oncall";
// Calls to the API: builds requests from view-model filters and adapts the
// responses. Pages call these through the hooks in queries.ts.
import type { MessageInitShape } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import type { TimeRangeSchema } from "../gen/natslens/v1/common_pb";
import { serverNow, setServerNow } from "../lib/clock";
import { bucketFor, presetMs, resolveRange, type RangeSpec } from "../lib/timeRange";
import { entityKindEnum, ms, n, series, window, severityEnum, sourceEnum } from "./adapt-common";
import { consumer, consumerDetail, connection, overview, server, stream, streamDetail } from "./adapt-entities";
import { acknowledgement, event, finding, findingStateEnum, mute, timeline, transition } from "./adapt-findings";
import { lensInfo } from "./adapt-info";
import { shellStatus } from "./adapt-shell";
import { FILTER_ENUM, matrix, type MatrixFilter } from "./placementMatrix";
import { replay, type Replay } from "./adapt-replay";
import { topology } from "./topology";
import { accountsAndAuth } from "./accounts";
import { capacity } from "./capacity";
import { dataHistory } from "./history";
import { microServices } from "./micro";
import { applications } from "./applications";
import { FILTER_ENUM as SUBJECT_FILTER_ENUM, subjectMatches, subjects, type SubjectFilter } from "./subjects";
import { lens } from "./client";
import type {
  ConnectionPage,
  Entity,
  EventSourceName,
  FindingState,
  LensEvent,
  Page,
  Finding,
  Mute,
  SeverityLevel,
} from "./model";

type RangeInit = MessageInitShape<typeof TimeRangeSchema>;

/**
 * The system a request reads, as useSystem() gives it. An empty one names
 * no system, and the server reads its default one.
 */
export function systemInit(sys: string): { system?: string } {
  return sys ? { system: sys } : {};
}

/** A live range leaves the end unset, so the server reads up to its now. */
export function rangeInit(spec: RangeSpec, now = serverNow()): RangeInit {
  if (spec.kind === "live") return { start: timestampFromMs(now - presetMs(spec.preset)) };
  return { start: timestampFromMs(spec.from), end: timestampFromMs(spec.to) };
}

export function entityInit(e: Entity | undefined) {
  if (!e) return undefined;
  return {
    type: entityKindEnum(e.kind),
    key: e.key,
    name: e.name,
    account: e.account,
    stream: e.stream,
    consumer: e.consumer,
    server: e.server,
  };
}

/** System-wide chart annotations, without Overview tiles or finding summaries. */
export async function getTimeWindows(sys: string, spec: RangeSpec, signal?: AbortSignal) {
  const result = await lens.getTimeWindows({ ...systemInit(sys), range: rangeInit(spec) }, { signal });
  return result.windows.map(window);
}

export async function getOverview(sys: string, spec: RangeSpec, signal?: AbortSignal) {
  return overview(await lens.getOverview({ ...systemInit(sys), range: rangeInit(spec), maxPoints: 96 }, { signal }));
}

export interface FindingFilter {
  states?: FindingState[];
  severities?: SeverityLevel[];
  includeMuted?: boolean;
  checkCode?: string;
  entity?: Entity;
  /** Only findings whose check or entity contains this text, ignoring case. */
  text?: string;
  pageSize?: number;
  pageToken?: string;
}

export async function listFindings(sys: string, spec: RangeSpec, f: FindingFilter, signal?: AbortSignal): Promise<Page<Finding>> {
  const r = await lens.listFindings(
    {
      ...systemInit(sys),
      range: rangeInit(spec),
      states: (f.states ?? []).map(findingStateEnum),
      severities: (f.severities ?? []).map(severityEnum),
      includeMuted: f.includeMuted ?? false,
      checkCode: f.checkCode ?? "",
      entity: entityInit(f.entity),
      text: f.text ?? "",
      pageSize: f.pageSize ?? 0,
      pageToken: f.pageToken ?? "",
    },
    { signal },
  );
  return { items: r.findings.map(finding), nextPageToken: r.nextPageToken, totalCount: r.totalCount };
}

export async function getFinding(sys: string, id: string, signal?: AbortSignal) {
  const r = await lens.getFinding({ ...systemInit(sys), id }, { signal });
  return { finding: r.finding ? finding(r.finding) : undefined, transitions: r.transitions.map(transition) };
}

export async function listMutes(sys: string, includeInactive: boolean, pageToken: string, signal?: AbortSignal): Promise<Page<Mute>> {
  const r = await lens.listMutes({ ...systemInit(sys), includeInactive, pageSize: 50, pageToken }, { signal });
  return { items: r.mutes.map((m) => mute(m)!), nextPageToken: r.nextPageToken, totalCount: r.totalCount };
}

export interface MuteInput {
  checkCode: string;
  entity?: Entity;
  reason: string;
  expiresAt: number;
  mutedBy: string;
  /** Accept the finding as by design: expiresAt is the review date. */
  accept?: boolean;
}

export async function muteFinding(sys: string, m: MuteInput) {
  const r = await lens.muteFinding({
    ...systemInit(sys),
    checkCode: m.checkCode,
    entity: entityInit(m.entity),
    reason: m.reason,
    expiresAt: timestampFromMs(m.expiresAt),
    mutedBy: m.mutedBy,
    accept: m.accept ?? false,
  });
  return mute(r.mute);
}

export async function unmuteFinding(sys: string, muteId: string, unmutedBy: string) {
  const r = await lens.unmuteFinding({ ...systemInit(sys), muteId, unmutedBy });
  return mute(r.mute);
}

/** The status timeline; includeOk adds the checks that were OK in every bucket. */
export async function getTimeline(sys: string, spec: RangeSpec, entity: Entity | undefined, signal?: AbortSignal, includeOk = false) {
  const { start, end } = resolveRange(spec, serverNow());
  const r = await lens.getStatusTimeline(
    { ...systemInit(sys), range: rangeInit(spec), bucket: durationFromMs(bucketFor(end - start)), entity: entityInit(entity), includeOk },
    { signal },
  );
  return timeline(r);
}

export interface EventFilter {
  sources?: EventSourceName[];
  kinds?: string[];
  entity?: Entity;
  clientName?: string;
  /** Only events whose kind or names contain this text, ignoring case. */
  text?: string;
  pageSize?: number;
  pageToken?: string;
}

export async function listEvents(sys: string, spec: RangeSpec, f: EventFilter, signal?: AbortSignal): Promise<Page<LensEvent>> {
  const r = await lens.listEvents(
    {
      ...systemInit(sys),
      range: rangeInit(spec),
      sources: (f.sources ?? []).map(sourceEnum),
      kinds: f.kinds ?? [],
      entity: entityInit(f.entity),
      clientName: f.clientName ?? "",
      text: f.text ?? "",
      pageSize: f.pageSize ?? 0,
      pageToken: f.pageToken ?? "",
    },
    { signal },
  );
  return { items: r.events.map(event), nextPageToken: r.nextPageToken, totalCount: r.totalCount };
}

export async function getEvent(sys: string, id: string, signal?: AbortSignal,readId?:string) {
  const r = await lens.getEvent({ ...systemInit(sys), id,eventReadId:parseEventReadId(readId) }, { signal });
 if(!r.event||r.event.id!==id)throw new Error("This source did not return the selected recorded event.");
 if(readId!==undefined&&r.eventReadId!==parseEventReadId(readId))throw new Error("This server did not acknowledge the event membership boundary.");
  return r.event ? event(r.event) : undefined;
}

/** Page reads are bounded. Search and filter callers explicitly traverse all pages. */
export async function listStreams(sys: string, spec: RangeSpec, includeKv: boolean, signal?: AbortSignal, page?: { pageToken: string }) {
  const request = { ...systemInit(sys), range: page?.pageToken ? undefined : rangeInit(spec), includeKv, summaryOnly: !page, limit: 100, pageToken: page?.pageToken ?? "" };
  let r = await lens.listStreams(request, { signal });
  const streams = [...r.streams];
  const totals = r.totals;
  const totalCount = r.totalCount;
  if (!page) {
    while (r.nextPageToken) {
      signal?.throwIfAborted();
      r = await lens.listStreams({ ...request, range: r.range, pageToken: r.nextPageToken }, { signal });
      streams.push(...r.streams);
    }
  }
  return {
    streams: streams.map(stream), totalCount, nextPageToken: r.nextPageToken, rangeEnd:ms(r.range?.end), epoch:String(r.epoch),
    totals: { bytes: n(totals?.bytes), durableConsumers: totals?.durableConsumers ?? 0, ephemeralConsumers: totals?.ephemeralConsumers ?? 0 },
  };
}

export async function getStream(sys: string, spec: RangeSpec, account: string, name: string, signal?: AbortSignal) {
  return streamDetail(await lens.getStream({ ...systemInit(sys), account, stream: name, range: rangeInit(spec) }, { signal }));
}

export async function listConsumers(
  sys: string,
  spec: RangeSpec,
  f: { account?: string; stream?: string; includeEphemeral?: boolean },
  signal?: AbortSignal,
  page?: { pageToken: string },
) {
  const request = { ...systemInit(sys), range: page?.pageToken ? undefined : rangeInit(spec), account: f.account ?? "", stream: f.stream ?? "", includeEphemeral: f.includeEphemeral ?? false, limit: 100, pageToken: page?.pageToken ?? "" };
  let r = await lens.listConsumers(request, { signal });
  const consumers = [...r.consumers];
  const totalCount = r.totalCount;
  if (!page) {
    while (r.nextPageToken) {
      signal?.throwIfAborted();
      r = await lens.listConsumers({ ...request, range: r.range, pageToken: r.nextPageToken }, { signal });
      consumers.push(...r.consumers);
    }
  }
  return Object.assign(consumers.map(consumer), { totalCount, nextPageToken: r.nextPageToken });
}

export async function getConsumer(sys: string, spec: RangeSpec, account: string, streamName: string, name: string, signal?: AbortSignal) {
  return consumerDetail(
    await lens.getConsumer({ ...systemInit(sys), account, stream: streamName, consumer: name, range: rangeInit(spec) }, { signal }),
  );
}

/** The story between moments a and b; the view model records what was asked. */
export async function getReplay(sys: string, a: number, b: number, signal?: AbortSignal): Promise<Replay> {
  return replay(await lens.getReplay({ ...systemInit(sys), a: timestampFromMs(a), b: timestampFromMs(b) }, { signal }), { system: sys, a, b });
}

export async function getPlacement(sys: string, spec: RangeSpec, filter: MatrixFilter, signal?: AbortSignal, selected?: { asset: string; server: string }) {
  return matrix(await lens.getPlacement({ ...systemInit(sys), range: rangeInit(spec), filter: FILTER_ENUM[filter], selectedAsset: selected?.asset, selectedServer: selected?.server }, { signal }));
}

/** How the servers connect, in the range's newest collection. */
export async function getTopology(sys: string, spec: RangeSpec, signal?: AbortSignal) {
  return topology(await lens.getTopology({ ...systemInit(sys), range: rangeInit(spec) }, { signal }));
}

/** What each stream keeps and bounds it, and when the stores fill, in the range's newest collection. */
export async function getCapacity(sys: string, spec: RangeSpec, signal?: AbortSignal) {
  return capacity(await lens.getCapacity({ ...systemInit(sys), range: rangeInit(spec) }, { signal }));
}

/** Where messages can go in the range's newest collection: stream subjects and client subscriptions of the account ("" for all). */
export async function getSubjects(sys: string, spec: RangeSpec, account: string, filter: SubjectFilter, signal?: AbortSignal) {
  return subjects(await lens.getSubjects({ ...systemInit(sys), range: rangeInit(spec), account, filter: SUBJECT_FILTER_ENUM[filter] }, { signal }));
}

/** The services built on the NATS micro framework in the range's newest collection, of the account ("" for all). */
export async function getMicroServices(sys: string, spec: RangeSpec, account: string, signal?: AbortSignal) {
  return microServices(await lens.getMicroServices({ ...systemInit(sys), range: rangeInit(spec), account }, { signal }));
}

/** The client applications of the range's newest collection, of the account ("" for all); tooling ones only when asked. */
export async function getApplications(sys: string, spec: RangeSpec, account: string, includeTooling: boolean, signal?: AbortSignal) {
  return applications(await lens.getApplications({ ...systemInit(sys), range: rangeInit(spec), account, includeTooling }, { signal }));
}

/** What a message on a literal subject would reach in the range's newest collection. */
export async function matchSubject(sys: string, spec: RangeSpec, account: string, subject: string, signal?: AbortSignal) {
  return subjectMatches(await lens.matchSubject({ ...systemInit(sys), range: rangeInit(spec), account, subject }, { signal }));
}

/** What nats-lens holds of the system: its history, the range's coverage and gaps, the archive and what uses the history. */
export async function getDataHistory(sys: string, spec: RangeSpec, signal?: AbortSignal, server = "") {
  const now = serverNow();
  const range = rangeInit(spec, now);
  const sent = resolveRange(spec, now);
  return dataHistory(await lens.getDataHistory({ ...systemInit(sys), range, server }, { signal }), sent);
}

/** The accounts of the range's newest collection, the failed logins of the 24 hours before it, and what expires soon. */
export async function getAccountsAndAuth(sys: string, spec: RangeSpec, signal?: AbortSignal) {
  return accountsAndAuth(await lens.getAccountsAndAuth({ ...systemInit(sys), range: rangeInit(spec) }, { signal }));
}

export async function listServers(sys: string, spec: RangeSpec, signal?: AbortSignal) {
  const r = await lens.listServers({ ...systemInit(sys), range: rangeInit(spec) }, { signal });
  return { servers: r.servers.map(server), expected: r.expected, missing: r.missing };
}

export interface ConnectionFilter {
  server?: string;
  account?: string;
  nameContains?: string;
  includeTooling?: boolean;
  pageSize?: number;
  pageToken?: string;
}

export async function listConnections(sys: string, spec: RangeSpec, f: ConnectionFilter, signal?: AbortSignal): Promise<ConnectionPage> {
  const r = await lens.listConnections(
    {
      ...systemInit(sys),
      range: rangeInit(spec),
      server: f.server ?? "",
      account: f.account ?? "",
      nameContains: f.nameContains ?? "",
      includeTooling: f.includeTooling ?? false,
      pageSize: f.pageSize ?? 0,
      pageToken: f.pageToken ?? "",
    },
    { signal },
  );
  return {
    items: r.connections.map(connection),
    nextPageToken: r.nextPageToken,
    totalCount: r.totalCount,
    epoch: String(r.epoch),
    toolingHidden: r.toolingHidden,
    servers: r.servers,
  };
}

export async function getSeries(sys: string, spec: RangeSpec, metric: string, entity: Entity | undefined, maxPoints: number, signal?: AbortSignal) {
  const r = await lens.getTimeSeries({ ...systemInit(sys), metric, entity: entityInit(entity), range: rangeInit(spec), maxPoints }, { signal });
  return series(r.series);
}

export async function getLensInfo(sys: string, signal?: AbortSignal) {
  return lensInfo(await lens.getLensInfo({ ...systemInit(sys) }, { signal }));
}

/** How long the shell waits for the server's now before it uses the browser's clock. */
export const CLOCK_TIMEOUT_MS = 3_000;

/**
 * Sets the UI's clock from the server's now and returns that now; a server
 * that reports none, or does not answer within timeoutMs, leaves the
 * browser's clock. With `now_fixed` the server's now does not move, so the
 * clock stops.
 */
export async function syncClock(signal?: AbortSignal, timeoutMs = CLOCK_TIMEOUT_MS): Promise<number | null> {
  const ctl = new AbortController();
  const stop = () => ctl.abort();
  if (signal?.aborted) stop();
  signal?.addEventListener("abort", stop);
  const timer = setTimeout(stop, timeoutMs);
  try {
    const r = await lens.getLensInfo({}, { signal: ctl.signal, timeoutMs });
    const at = ms(r.now);
    // The receive time is the browser's own clock, which the offset is taken from.
    if (at !== undefined) setServerNow(at, Date.now(), r.nowFixed);
    return at ?? null;
  } finally {
    clearTimeout(timer);
    signal?.removeEventListener("abort", stop);
  }
}

/**
 * What the shell needs on every page: freshness, the servers and the
 * findings tally of the system, and every system's badge. Without start
 * the server reads the last two collect intervals; with it, the range from
 * start to now.
 */
export async function getShellStatus(sys: string, start?: number, signal?: AbortSignal) {
  return shellStatus(await lens.getShellStatus({ ...systemInit(sys), range: start === undefined ? undefined : { start: timestampFromMs(start) } }, { signal }));
}

/** How far back the header looks for the last data of a stopped collector. */
export const LAST_DATA_LOOKBACK_MS = 2 * 86_400_000;

/**
 * The newest collection before the collector stopped at since, with its
 * servers: the shell's status from three intervals before the stop, at
 * most LAST_DATA_LOOKBACK_MS back, so a long outage never asks for weeks.
 */
export async function getLastData(sys: string, since: number, intervalMs: number, signal?: AbortSignal) {
  const start = Math.max(since - 3 * intervalMs, serverNow() - LAST_DATA_LOOKBACK_MS);
  const s = await getShellStatus(sys, start, signal);
  return { freshness: s.freshness, servers: s.servers };
}

export interface AckInput {
  findingId: string;
  note: string;
  quietMs?: number;
  untilResolved?: boolean;
  ackedBy: string;
}

export async function acknowledgeFinding(sys: string, a: AckInput) {
  const r = await lens.acknowledgeFinding({ ...systemInit(sys), findingId: a.findingId, note: a.note, quietFor: a.quietMs === undefined ? undefined : durationFromMs(a.quietMs), untilResolved: a.untilResolved ?? false, ackedBy: a.ackedBy });
  return acknowledgement(r.acknowledgement)!;
}

export async function endAcknowledgement(sys: string, id: string, endedBy: string) {
  const r = await lens.endAcknowledgement({ ...systemInit(sys), id, endedBy });
  return acknowledgement(r.acknowledgement)!;
}

export async function getOnCall(sys: string, id: string, signal?: AbortSignal) {
  return onCall(await lens.getOnCall({ ...systemInit(sys), findingId: id }, { signal }));
}

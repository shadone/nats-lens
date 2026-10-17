// Calls to the API: builds requests from view-model filters and adapts the
// responses. Pages call these through the hooks in queries.ts.
import type { MessageInitShape } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import type { TimeRangeSchema } from "../gen/natslens/v1/common_pb";
import { bucketFor, presetMs, resolveRange, type RangeSpec } from "../lib/timeRange";
import { entityKindEnum, n, series, severityEnum, sourceEnum } from "./adapt-common";
import { consumer, consumerDetail, connection, overview, server, stream, streamDetail } from "./adapt-entities";
import { event, finding, findingStateEnum, mute, timeline, transition } from "./adapt-findings";
import { lensInfo } from "./adapt-info";
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
export function rangeInit(spec: RangeSpec, now = Date.now()): RangeInit {
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

export async function getOverview(sys: string, spec: RangeSpec, signal?: AbortSignal) {
  return overview(await lens.getOverview({ ...systemInit(sys), range: rangeInit(spec), maxPoints: 96 }, { signal }));
}

export interface FindingFilter {
  states?: FindingState[];
  severities?: SeverityLevel[];
  includeMuted?: boolean;
  checkCode?: string;
  entity?: Entity;
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
}

export async function muteFinding(sys: string, m: MuteInput) {
  const r = await lens.muteFinding({
    ...systemInit(sys),
    checkCode: m.checkCode,
    entity: entityInit(m.entity),
    reason: m.reason,
    expiresAt: timestampFromMs(m.expiresAt),
    mutedBy: m.mutedBy,
  });
  return mute(r.mute);
}

export async function unmuteFinding(sys: string, muteId: string, unmutedBy: string) {
  const r = await lens.unmuteFinding({ ...systemInit(sys), muteId, unmutedBy });
  return mute(r.mute);
}

export async function getTimeline(sys: string, spec: RangeSpec, entity: Entity | undefined, signal?: AbortSignal) {
  const { start, end } = resolveRange(spec, Date.now());
  const r = await lens.getStatusTimeline(
    { ...systemInit(sys), range: rangeInit(spec), bucket: durationFromMs(bucketFor(end - start)), entity: entityInit(entity) },
    { signal },
  );
  return timeline(r);
}

export interface EventFilter {
  sources?: EventSourceName[];
  kinds?: string[];
  entity?: Entity;
  clientName?: string;
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
      pageSize: f.pageSize ?? 0,
      pageToken: f.pageToken ?? "",
    },
    { signal },
  );
  return { items: r.events.map(event), nextPageToken: r.nextPageToken, totalCount: r.totalCount };
}

export async function getEvent(sys: string, id: string, signal?: AbortSignal) {
  const r = await lens.getEvent({ ...systemInit(sys), id }, { signal });
  return r.event ? event(r.event) : undefined;
}

export async function listStreams(sys: string, spec: RangeSpec, includeKv: boolean, signal?: AbortSignal) {
  const r = await lens.listStreams({ ...systemInit(sys), range: rangeInit(spec), includeKv }, { signal });
  return {
    streams: r.streams.map(stream),
    totals: {
      bytes: n(r.totals?.bytes),
      durableConsumers: r.totals?.durableConsumers ?? 0,
      ephemeralConsumers: r.totals?.ephemeralConsumers ?? 0,
    },
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
) {
  const r = await lens.listConsumers(
    { ...systemInit(sys), range: rangeInit(spec), account: f.account ?? "", stream: f.stream ?? "", includeEphemeral: f.includeEphemeral ?? false },
    { signal },
  );
  return r.consumers.map(consumer);
}

export async function getConsumer(sys: string, spec: RangeSpec, account: string, streamName: string, name: string, signal?: AbortSignal) {
  return consumerDetail(
    await lens.getConsumer({ ...systemInit(sys), account, stream: streamName, consumer: name, range: rangeInit(spec) }, { signal }),
  );
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
  };
}

export async function getSeries(sys: string, spec: RangeSpec, metric: string, entity: Entity | undefined, maxPoints: number, signal?: AbortSignal) {
  const r = await lens.getTimeSeries({ ...systemInit(sys), metric, entity: entityInit(entity), range: rangeInit(spec), maxPoints }, { signal });
  return series(r.series);
}

export async function getLensInfo(sys: string, signal?: AbortSignal) {
  return lensInfo(await lens.getLensInfo({ ...systemInit(sys) }, { signal }));
}

/**
 * What the shell needs on every page: freshness, the servers now and the
 * findings tally. It reads GetOverview over the last two collect intervals
 * with one point per tile, which keeps the call small; the API has no
 * lighter call that names the newest collection's time.
 */
export async function getHeaderStatus(sys: string, intervalMs: number, signal?: AbortSignal) {
  const o = overview(await lens.getOverview({ ...systemInit(sys), range: { start: timestampFromMs(Date.now() - 2 * intervalMs) }, maxPoints: 1 }, { signal }));
  return { cluster: o.cluster, freshness: o.freshness, servers: o.servers, tally: o.tally };
}

/** How far back the header looks for the last data of a stopped collector. */
export const LAST_DATA_LOOKBACK_MS = 2 * 86_400_000;

/**
 * The newest collection before the collector stopped at since, with its
 * servers: GetOverview from three intervals before the stop, at most
 * LAST_DATA_LOOKBACK_MS back, so a long outage never asks for weeks.
 */
export async function getLastData(sys: string, since: number, intervalMs: number, signal?: AbortSignal) {
  const start = Math.max(since - 3 * intervalMs, Date.now() - LAST_DATA_LOOKBACK_MS);
  const o = overview(await lens.getOverview({ ...systemInit(sys), range: { start: timestampFromMs(start) }, maxPoints: 1 }, { signal }));
  return { freshness: o.freshness, servers: o.servers };
}

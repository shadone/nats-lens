// Calls to the API: builds requests from view-model filters and adapts the
// responses. Pages call these through the hooks in queries.ts.
import type { MessageInitShape } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import type { TimeRangeSchema } from "../gen/natslens/v1/common_pb";
import { bucketFor, presetMs, resolveRange, type RangeSpec } from "../lib/timeRange";
import { entityKindEnum, n, series, severityEnum, sourceEnum } from "./adapt-common";
import { consumer, consumerDetail, connection, overview, server, serverDetail, stream, streamDetail } from "./adapt-entities";
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
 * The system the page shows: the ?system= of its URL, passed through as
 * given. Without it the request names no system and the server reads its
 * default one.
 */
function system(): { system?: string } {
  const id = new URLSearchParams(window.location.search).get("system");
  return id ? { system: id } : {};
}

/** A live range leaves the end unset, so the server reads up to its now. */
export function rangeInit(spec: RangeSpec, now = Date.now()): RangeInit {
  if (spec.kind === "live") return { start: timestampFromMs(now - presetMs(spec.preset)) };
  return { start: timestampFromMs(spec.from), end: timestampFromMs(spec.to) };
}

function entityInit(e: Entity | undefined) {
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

export async function getOverview(spec: RangeSpec, signal?: AbortSignal) {
  return overview(await lens.getOverview({ ...system(), range: rangeInit(spec), maxPoints: 96 }, { signal }));
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

export async function listFindings(spec: RangeSpec, f: FindingFilter, signal?: AbortSignal): Promise<Page<Finding>> {
  const r = await lens.listFindings(
    {
      ...system(),
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

export async function getFinding(id: string, signal?: AbortSignal) {
  const r = await lens.getFinding({ ...system(), id }, { signal });
  return { finding: r.finding ? finding(r.finding) : undefined, transitions: r.transitions.map(transition) };
}

export async function listMutes(includeInactive: boolean, pageToken: string, signal?: AbortSignal): Promise<Page<Mute>> {
  const r = await lens.listMutes({ ...system(), includeInactive, pageSize: 50, pageToken }, { signal });
  return { items: r.mutes.map((m) => mute(m)!), nextPageToken: r.nextPageToken, totalCount: r.totalCount };
}

export interface MuteInput {
  checkCode: string;
  entity?: Entity;
  reason: string;
  expiresAt: number;
  mutedBy: string;
}

export async function muteFinding(m: MuteInput) {
  const r = await lens.muteFinding({
    ...system(),
    checkCode: m.checkCode,
    entity: entityInit(m.entity),
    reason: m.reason,
    expiresAt: timestampFromMs(m.expiresAt),
    mutedBy: m.mutedBy,
  });
  return mute(r.mute);
}

export async function unmuteFinding(muteId: string, unmutedBy: string) {
  const r = await lens.unmuteFinding({ ...system(), muteId, unmutedBy });
  return mute(r.mute);
}

export async function getTimeline(spec: RangeSpec, entity: Entity | undefined, signal?: AbortSignal) {
  const { start, end } = resolveRange(spec, Date.now());
  const r = await lens.getStatusTimeline(
    { ...system(), range: rangeInit(spec), bucket: durationFromMs(bucketFor(end - start)), entity: entityInit(entity) },
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

export async function listEvents(spec: RangeSpec, f: EventFilter, signal?: AbortSignal): Promise<Page<LensEvent>> {
  const r = await lens.listEvents(
    {
      ...system(),
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

export async function getEvent(id: string, signal?: AbortSignal) {
  const r = await lens.getEvent({ ...system(), id }, { signal });
  return r.event ? event(r.event) : undefined;
}

export async function listStreams(spec: RangeSpec, includeKv: boolean, signal?: AbortSignal) {
  const r = await lens.listStreams({ ...system(), range: rangeInit(spec), includeKv }, { signal });
  return {
    streams: r.streams.map(stream),
    totals: {
      bytes: n(r.totals?.bytes),
      durableConsumers: r.totals?.durableConsumers ?? 0,
      ephemeralConsumers: r.totals?.ephemeralConsumers ?? 0,
    },
  };
}

export async function getStream(spec: RangeSpec, account: string, name: string, signal?: AbortSignal) {
  return streamDetail(await lens.getStream({ ...system(), account, stream: name, range: rangeInit(spec) }, { signal }));
}

export async function listConsumers(
  spec: RangeSpec,
  f: { account?: string; stream?: string; includeEphemeral?: boolean },
  signal?: AbortSignal,
) {
  const r = await lens.listConsumers(
    { ...system(), range: rangeInit(spec), account: f.account ?? "", stream: f.stream ?? "", includeEphemeral: f.includeEphemeral ?? false },
    { signal },
  );
  return r.consumers.map(consumer);
}

export async function getConsumer(spec: RangeSpec, account: string, streamName: string, name: string, signal?: AbortSignal) {
  return consumerDetail(
    await lens.getConsumer({ ...system(), account, stream: streamName, consumer: name, range: rangeInit(spec) }, { signal }),
  );
}

export async function listServers(spec: RangeSpec, signal?: AbortSignal) {
  const r = await lens.listServers({ ...system(), range: rangeInit(spec) }, { signal });
  return { servers: r.servers.map(server), expected: r.expected, missing: r.missing };
}

export async function getServer(spec: RangeSpec, name: string, signal?: AbortSignal) {
  return serverDetail(await lens.getServer({ ...system(), name, range: rangeInit(spec) }, { signal }));
}

export interface ConnectionFilter {
  server?: string;
  account?: string;
  nameContains?: string;
  includeTooling?: boolean;
  pageSize?: number;
  pageToken?: string;
}

export async function listConnections(spec: RangeSpec, f: ConnectionFilter, signal?: AbortSignal): Promise<ConnectionPage> {
  const r = await lens.listConnections(
    {
      ...system(),
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

export async function getSeries(spec: RangeSpec, metric: string, entity: Entity | undefined, maxPoints: number, signal?: AbortSignal) {
  const r = await lens.getTimeSeries({ ...system(), metric, entity: entityInit(entity), range: rangeInit(spec), maxPoints }, { signal });
  return series(r.series);
}


export async function getLensInfo(signal?: AbortSignal) {
  return lensInfo(await lens.getLensInfo({ ...system() }, { signal }));
}

/**
 * What the header needs on every page: freshness and the servers now. It
 * reads GetOverview over the last two collect intervals with one point per
 * tile, which keeps the call small; the API has no lighter call that names
 * the newest epoch's time.
 */
export async function getHeaderStatus(intervalMs: number, signal?: AbortSignal) {
  const o = overview(await lens.getOverview({ ...system(), range: { start: timestampFromMs(Date.now() - 2 * intervalMs) }, maxPoints: 1 }, { signal }));
  return { cluster: o.cluster, freshness: o.freshness, servers: o.servers };
}

// Servers of one system: requests, view models and hooks for the Servers
// page and a server's own page. Every hook keys on the page's system.
import type { Duration } from "@bufbuild/protobuf/wkt";
import { useQuery } from "@tanstack/react-query";
import { keepSameSystem } from "./placeholder";
import { EntityType } from "../gen/natslens/v1/common_pb";
import type { GetServerResponse, ListServersResponse, ServerSummary } from "../gen/natslens/v1/servers_pb";
import type { Series as PSeries } from "../gen/natslens/v1/timeseries_pb";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { dur, entity, ms, optN, series } from "./adapt-common";
import { server, serverDetail } from "./adapt-entities";
import { event } from "./adapt-findings";
import { lens } from "./client";
import type { LensEvent, Point, Server, ServerDetail, Series } from "./model";
import { rangeInit, systemInit } from "./requests";
import { useTrends, type TrendQuery } from "./trendBatch";

/** A server with what the Servers page adds to the shared view model. Numbers not observed are unset. */
export interface ServerRow extends Server {
  streamLeaders?: number;
  consumerLeaders?: number;
  slowConsumersInRange?: number;
  tags: string[];
}

export interface ServerList {
  servers: ServerRow[];
  expected: string[];
  missing: string[];
}

export interface ServerLimits {
  maxPayload?: number;
  maxPending?: number;
  writeDeadlineMs?: number;
  pingIntervalMs?: number;
  maxPingsOut?: number;
  jsMaxStorage?: number;
  jsMaxMemory?: number;
  maxConnections?: number;
}

export interface RouteView {
  remoteName: string;
  rttMs?: number;
  pendingBytes?: number;
}

export interface KubernetesPlacement {
  pod: string;
  namespace: string;
  node: string;
  scheduledAt?: number;
  restartsInRange: number;
}

export interface ServerInfo extends Omit<ServerDetail, "server"> {
  server?: ServerRow;
  routeViews: RouteView[];
  gateways?: number;
  leafnodes?: number;
  limits?: ServerLimits;
  /** API field names, for example max_payload. */
  limitsDiffer: string[];
  kubernetes?: KubernetesPlacement;
  configLoadedAt?: number;
}

export interface BandPoint extends Point {
  low?: number;
  high?: number;
}

/** A series whose points may carry a band, with the busiest server's own line. */
export interface BandSeries extends Series {
  points: BandPoint[];
  top?: string;
  topPoints: Point[];
}

/** A duration in milliseconds with its fraction, which route round trips need: 0.47 ms, not 0. */
function preciseMs(d: Duration | undefined): number | undefined {
  return d ? Number(d.seconds) * 1000 + d.nanos / 1e6 : undefined;
}

export function serverRow(s: ServerSummary): ServerRow {
  return {
    ...server(s),
    streamLeaders: s.streamLeaders,
    consumerLeaders: s.consumerLeaders,
    slowConsumersInRange: optN(s.slowConsumersInRange),
    tags: s.tags,
  };
}

export function serverList(r: ListServersResponse): ServerList {
  return { servers: r.servers.map(serverRow), expected: r.expected, missing: r.missing };
}

export function serverInfo(r: GetServerResponse): ServerInfo {
  const l = r.limits;
  const k = r.kubernetes;
  return {
    ...serverDetail(r),
    server: r.server ? serverRow(r.server) : undefined,
    routeViews: r.routeViews.map((v) => ({ remoteName: v.remoteName, rttMs: preciseMs(v.rtt), pendingBytes: optN(v.pendingBytes) })),
    gateways: r.gateways,
    leafnodes: r.leafnodes,
    limits: l && {
      maxPayload: optN(l.maxPayload),
      maxPending: optN(l.maxPending),
      writeDeadlineMs: dur(l.writeDeadline),
      pingIntervalMs: dur(l.pingInterval),
      maxPingsOut: l.maxPingsOut,
      jsMaxStorage: optN(l.jsMaxStorage),
      jsMaxMemory: optN(l.jsMaxMemory),
      maxConnections: l.maxConnections,
    },
    limitsDiffer: r.limitsDiffer,
    kubernetes: k && { pod: k.pod, namespace: k.namespace, node: k.node, scheduledAt: ms(k.scheduledAt), restartsInRange: k.restartsInRange },
    configLoadedAt: ms(r.configLoadedAt),
  };
}

export function bandSeries(s: PSeries | undefined): BandSeries | undefined {
  const base = series(s);
  if (!s || !base) return undefined;
  return {
    ...base,
    points: s.points.map((p, i) => ({ ...base.points[i], low: p.low, high: p.high })),
    top: entity(s.top)?.name || undefined,
    topPoints: s.topPoints.map((p) => ({ time: ms(p.time) ?? 0, value: p.value })),
  };
}

export async function listServerRows(sys: string, spec: RangeSpec, signal?: AbortSignal): Promise<ServerList> {
  return serverList(await lens.listServers({ ...systemInit(sys), range: rangeInit(spec) }, { signal }));
}

export async function getServerInfo(sys: string, spec: RangeSpec, name: string, signal?: AbortSignal): Promise<ServerInfo> {
  return serverInfo(await lens.getServer({ ...systemInit(sys), name, range: rangeInit(spec) }, { signal }));
}

/** A series of the system, or of one server when `serverName` is set. */
export async function getBandSeries(sys: string, spec: RangeSpec, metric: string, serverName: string | undefined, maxPoints: number, signal?: AbortSignal) {
  const ref = serverName ? { type: EntityType.SERVER, key: serverName, name: serverName, server: serverName } : undefined;
  const r = await lens.getTimeSeries({ ...systemInit(sys), metric, entity: ref, range: rangeInit(spec), maxPoints }, { signal });
  return bandSeries(r.series);
}

/** A recent event row: repeats folded into one, with how many it stands for. */
export interface RecentEvent extends LensEvent {
  repeats: number;
}

/** The newest events about a server, routine ones hidden and repeats folded. */
export async function listServerEvents(sys: string, spec: RangeSpec, name: string, pageSize: number, signal?: AbortSignal): Promise<RecentEvent[]> {
  const r = await lens.listEvents(
    {
      ...systemInit(sys),
      range: rangeInit(spec),
      entity: { type: EntityType.SERVER, key: name, name, server: name },
      pageSize,
      hideRoutine: true,
      foldRepeats: true,
    },
    { signal },
  );
  return r.events.map((e) => ({ ...event(e), repeats: e.repeats }));
}

function refetch(spec: RangeSpec) {
  return spec.kind === "live" ? LIVE_REFRESH_MS : false;
}

export function useServerList(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["serverList", sys, rangeKey(spec)],
    queryFn: ({ signal }) => listServerRows(sys, spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys),
  });
}

export function useServerInfo(spec: RangeSpec, name: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["serverInfo", sys, rangeKey(spec), name],
    queryFn: ({ signal }) => getServerInfo(sys, spec, name, signal),
    refetchInterval: refetch(spec),
  });
}

export function useBandSeries(spec: RangeSpec, metric: string, serverName?: string, maxPoints = 120) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["bandSeries", sys, rangeKey(spec), metric, serverName ?? "", maxPoints],
    queryFn: ({ signal }) => getBandSeries(sys, spec, metric, serverName, maxPoints, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys, { 3: metric, 4: serverName ?? "" }),
  });
}

export function useServerEvents(spec: RangeSpec, name: string, pageSize = 6) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["serverEvents", sys, rangeKey(spec), name, pageSize],
    queryFn: ({ signal }) => listServerEvents(sys, spec, name, pageSize, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys),
  });
}

/** Slow consumers per bucket in the Servers headline. */
export const SLOW_STRIP_QUERY: TrendQuery = { metric: "cluster.slow_consumers", maxPoints: 24 };

/** The series of the Servers page: the fleet charts and the slow-consumer strip. */
export const SERVERS_QUERIES: TrendQuery[] = [
  { metric: "cluster.cpu_percent" },
  { metric: "cluster.mem_bytes" },
  { metric: "cluster.connections" },
  { metric: "cluster.in_msgs_rate" },
  { metric: "cluster.out_msgs_rate" },
  SLOW_STRIP_QUERY,
];

/** The Servers page's series in one call; its parts share the query. */
export function useServersTrends(spec: RangeSpec) {
  return useTrends(spec, SERVERS_QUERIES);
}

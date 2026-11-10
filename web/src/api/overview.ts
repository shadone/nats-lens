// The Overview cockpit's requests, view models and hooks: band series for
// the trends, the busiest consumers, the streams with their placement and
// the current findings. Every hook keys on the page's system.
import { useQuery } from "@tanstack/react-query";
import { keepSameSystem } from "./placeholder";
import { ConsumerSort } from "../gen/natslens/v1/consumers_pb";
import type { StreamSummary as PStream } from "../gen/natslens/v1/streams_pb";
import type { Series as PSeries } from "../gen/natslens/v1/timeseries_pb";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { entity as entityOf, ms, series } from "./adapt-common";
import { consumer, stream } from "./adapt-entities";
import { placement, type Placement } from "./adapt-placement";
import { lens } from "./client";
import type { ConsumerSummary, Entity, Point, Series, StreamSummary, StreamTotals } from "./model";
import { entityInit, listFindings, rangeInit, systemInit } from "./requests";

/** A point with a spread: for a band metric the median as value and the lowest and highest server as low and high; for a count the bucket's last value and its lowest and highest. */
export interface BandPoint extends Point {
  low?: number;
  high?: number;
}

/** A series whose points may carry a band, with the busiest server and its own line. */
export interface CounterComparisons {observedIncrease?:string;comparisonCount:bigint;serverIdCount:bigint;firstComparedAt?:number;lastComparedAt?:number;excludedReports:bigint;boundaryReports:bigint}
export interface MetricObservations {
 counterComparisons?:CounterComparisons;
 sampleCount:bigint;knownCount:bigint;firstSampleAt?:number;lastSampleAt?:number;latest?:number;mean?:number;minimum?:number;maximum?:number;maximumAt?:number;observedTotal?:string;minimumLow?:number;maximumHigh?:number;
}
export interface TrendSeries extends Series {
  observations?:MetricObservations;
  points: BandPoint[];
  /** Name of the busiest server of a band metric. */
  top?: string;
  topPoints: Point[];
}

export function trendSeries(s: PSeries | undefined): TrendSeries | undefined {
  const base = series(s);
  if (!s || !base) return undefined;
  const top = entityOf(s.top);
  return {
    ...base,
    observations:s.observations?{...s.observations,firstSampleAt:ms(s.observations.firstSampleAt),lastSampleAt:ms(s.observations.lastSampleAt),maximumAt:ms(s.observations.maximumAt),counterComparisons:s.observations.counterComparisons?{...s.observations.counterComparisons,firstComparedAt:ms(s.observations.counterComparisons.firstComparedAt),lastComparedAt:ms(s.observations.counterComparisons.lastComparedAt)}:undefined}:undefined,
    points: s.points.map((p) => ({ time: ms(p.time) ?? 0, value: p.value, low: p.low, high: p.high })),
    top: top ? top.server || top.name || top.key : undefined,
    topPoints: s.topPoints.map((p) => ({ time: ms(p.time) ?? 0, value: p.value })),
  };
}

export interface ConsumerRate extends ConsumerSummary {
  /** Unset when the leader's sequence was not observed over the window. */
  deliveredPerSecond?: number;
}

export interface StreamPlacement extends StreamSummary {
  placement: Placement[];
}

function refetch(spec: RangeSpec) {
  return spec.kind === "live" ? LIVE_REFRESH_MS : false;
}

export async function listBusiestConsumers(sys: string, spec: RangeSpec, account: string, limit: number, signal?: AbortSignal): Promise<ConsumerRate[]> {
  const r = await lens.listConsumers(
    { ...systemInit(sys), range: rangeInit(spec), account, sort: ConsumerSort.DELIVERED_RATE, limit },
    { signal },
  );
  return r.consumers.map((c) => ({ ...consumer(c), deliveredPerSecond: c.deliveredPerSecond }));
}

/** The consumers with the highest delivery rate at the range's end, fastest first; no other account's stand in while they load. */
export function useBusiestConsumers(spec: RangeSpec, account: string, limit = 6) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["busiestConsumers", sys, rangeKey(spec), account, limit],
    queryFn: ({ signal }) => listBusiestConsumers(sys, spec, account, limit, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys, { 3: account }),
  });
}

export function streamPlacement(s: PStream): StreamPlacement {
  return { ...stream(s), placement: placement(s.placement) };
}

/** Streams the Overview lists before the link to all of them. */
export const STREAMS_SHOWN = 10;

/** The worst STREAMS_SHOWN streams, and how many streams match in all. */
export async function listStreamPlacement(
  sys: string,
  spec: RangeSpec,
  account: string,
  signal?: AbortSignal,
): Promise<{ streams: StreamPlacement[]; count: number; totals: StreamTotals }> {
  const r = await lens.listStreams({ ...systemInit(sys), range: rangeInit(spec), account, limit: STREAMS_SHOWN }, { signal });
  return {
    streams: r.streams.map(streamPlacement),
    count: r.totalCount,
    totals: {
      bytes: Number(r.totals?.bytes ?? 0),
      durableConsumers: r.totals?.durableConsumers ?? 0,
      ephemeralConsumers: r.totals?.ephemeralConsumers ?? 0,
    },
  };
}

/** The streams of the newest collection with one placement square per replica; no other account's stand in while they load. */
export function useStreamPlacement(spec: RangeSpec, account: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["streamPlacement", sys, rangeKey(spec), account],
    queryFn: ({ signal }) => listStreamPlacement(sys, spec, account, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys, { 3: account }),
  });
}

/** Most findings the cause cards read; a larger set is cut and says so. */
export const CURRENT_FINDINGS_LIMIT = 500;

/** The findings that fire at the range's end, muted ones included, worst first; with an account, that account's and those of no one account. No other account's stand in while they load. */
export function useCurrentFindings(spec: RangeSpec, account = "") {
  const sys = useSystem();
  return useQuery({
    queryKey: ["currentFindings", sys, rangeKey(spec), account],
    queryFn: ({ signal }) => listFindings(sys, spec, { includeMuted: true, pageSize: CURRENT_FINDINGS_LIMIT, account }, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys, { 3: account }),
  });
}

/** Where a stream or consumer sits: its leader and one entry per server that holds a replica. */
export interface AssetPlacement {
  reported: boolean;
  leader: string;
  replicas: Placement[];
}

/** Most assets one placement read asks for, as the API allows. */
export const ASSET_PLACEMENT_LIMIT = 200;

/** The placement of the streams and consumers, by entity key; unknown keys are absent. */
export async function listAssetPlacement(sys: string, spec: RangeSpec, entities: Entity[], signal?: AbortSignal): Promise<Map<string, AssetPlacement>> {
  const r = await lens.listAssetPlacement(
    { ...systemInit(sys), range: rangeInit(spec), entities: entities.slice(0, ASSET_PLACEMENT_LIMIT).map((e) => entityInit(e)!) },
    { signal },
  );
  return new Map(r.assets.map((a) => [a.entity?.key ?? "", { reported: a.reported, leader: a.leader, replicas: placement(a.replicas) }]));
}

/** The placement of the assets the cause cards list: one read for every card, none without a stream or consumer. */
export function useAssetPlacement(spec: RangeSpec, entities: Entity[]) {
  const sys = useSystem();
  const keys = entities.map((e) => `${e.kind}:${e.key}`);
  return useQuery({
    queryKey: ["assetPlacement", sys, rangeKey(spec), keys.join("\u0000")],
    queryFn: ({ signal }) => listAssetPlacement(sys, spec, entities, signal),
    enabled: entities.length > 0,
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys),
  });
}

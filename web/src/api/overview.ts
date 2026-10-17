// The Overview cockpit's requests, view models and hooks: band series for
// the trends, the busiest consumers, the streams with their placement and
// the current findings. Every hook keys on the page's system.
import { keepPreviousData, useQuery } from "@tanstack/react-query";
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

/** A point of a band metric: the median as value, the lowest and highest server as low and high. */
export interface BandPoint extends Point {
  low?: number;
  high?: number;
}

/** A series whose points may carry a band, with the busiest server and its own line. */
export interface TrendSeries extends Series {
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

/** Points per trend chart and per sparkline. */
export const TREND_POINTS = 96;

export async function getTrend(sys: string, spec: RangeSpec, metric: string, e: Entity | undefined, signal?: AbortSignal) {
  const r = await lens.getTimeSeries(
    { ...systemInit(sys), metric, entity: entityInit(e), range: rangeInit(spec), maxPoints: TREND_POINTS },
    { signal },
  );
  return trendSeries(r.series);
}

/** One metric over the range, with the band and busiest server when the metric has them. */
export function useTrend(spec: RangeSpec, metric: string, e?: Entity) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["trend", sys, rangeKey(spec), metric, e ? e.key : ""],
    queryFn: ({ signal }) => getTrend(sys, spec, metric, e, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export async function listBusiestConsumers(sys: string, spec: RangeSpec, account: string, limit: number, signal?: AbortSignal): Promise<ConsumerRate[]> {
  const r = await lens.listConsumers(
    { ...systemInit(sys), range: rangeInit(spec), account, sort: ConsumerSort.DELIVERED_RATE, limit },
    { signal },
  );
  return r.consumers.map((c) => ({ ...consumer(c), deliveredPerSecond: c.deliveredPerSecond }));
}

/** The consumers with the highest delivery rate at the range's end, fastest first. */
export function useBusiestConsumers(spec: RangeSpec, account: string, limit = 6) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["busiestConsumers", sys, rangeKey(spec), account, limit],
    queryFn: ({ signal }) => listBusiestConsumers(sys, spec, account, limit, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function streamPlacement(s: PStream): StreamPlacement {
  return { ...stream(s), placement: placement(s.placement) };
}

// TODO(limit): ListStreams has no limit, so the Overview fetches every stream to show
// the 10 worst; a server-side limit (and sort by status) would cut the reply.
export async function listStreamPlacement(sys: string, spec: RangeSpec, account: string, signal?: AbortSignal): Promise<{ streams: StreamPlacement[]; totals: StreamTotals }> {
  const r = await lens.listStreams({ ...systemInit(sys), range: rangeInit(spec), account }, { signal });
  return {
    streams: r.streams.map(streamPlacement),
    totals: {
      bytes: Number(r.totals?.bytes ?? 0),
      durableConsumers: r.totals?.durableConsumers ?? 0,
      ephemeralConsumers: r.totals?.ephemeralConsumers ?? 0,
    },
  };
}

/** The streams of the newest collection with one placement square per replica. */
export function useStreamPlacement(spec: RangeSpec, account: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["streamPlacement", sys, rangeKey(spec), account],
    queryFn: ({ signal }) => listStreamPlacement(sys, spec, account, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

/** Most findings the cause cards read; a larger set is cut and says so. */
export const CURRENT_FINDINGS_LIMIT = 500;

/** The findings that fire at the range's end, muted ones included, worst first. */
export function useCurrentFindings(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["currentFindings", sys, rangeKey(spec)],
    queryFn: ({ signal }) => listFindings(sys, spec, { includeMuted: true, pageSize: CURRENT_FINDINGS_LIMIT }, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

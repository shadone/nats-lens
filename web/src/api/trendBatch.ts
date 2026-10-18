// Several series over one range in one GetTimeSeriesBatch call, and the
// slice of one series in the shape a single query has, so that each chart
// shows its own data, error and loading state.
import { keepPreviousData, useQuery, type UseQueryResult } from "@tanstack/react-query";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { lens } from "./client";
import type { Entity } from "./model";
import { trendSeries, type TrendSeries } from "./overview";
import { entityInit, rangeInit, systemInit } from "./requests";

/** Points per trend chart and per sparkline. */
export const TREND_POINTS = 96;

/** One series of a batch. */
export interface TrendQuery {
  metric: string;
  entity?: Entity;
  /** Points of this series; unset means TREND_POINTS. */
  maxPoints?: number;
}

/** A series, or why the server did not serve it. */
export interface TrendResult {
  series?: TrendSeries;
  error?: string;
}

export async function getTrends(sys: string, spec: RangeSpec, queries: TrendQuery[], signal?: AbortSignal): Promise<TrendResult[]> {
  const r = await lens.getTimeSeriesBatch(
    {
      ...systemInit(sys),
      range: rangeInit(spec),
      maxPoints: TREND_POINTS,
      queries: queries.map((q) => ({ metric: q.metric, entity: entityInit(q.entity), maxPoints: q.maxPoints ?? 0 })),
    },
    { signal },
  );
  return r.results.map((x) => (x.error ? { error: x.error } : { series: trendSeries(x.series) }));
}

function queryKeyOf(q: TrendQuery): string {
  return `${q.metric}|${q.entity?.key ?? ""}|${q.maxPoints ?? ""}`;
}

/** The queries' series over the range, in one call; results are in query order. */
export function useTrends(spec: RangeSpec, queries: TrendQuery[]) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["trends", sys, rangeKey(spec), queries.map(queryKeyOf)],
    queryFn: ({ signal }) => getTrends(sys, spec, queries, signal),
    refetchInterval: spec.kind === "live" ? LIVE_REFRESH_MS : false,
    placeholderData: keepPreviousData,
    enabled: queries.length > 0,
  });
}

/** One series of a batch, shaped like a single query's result. */
export interface TrendSlice {
  data: TrendSeries | undefined;
  error: unknown;
  isPending: boolean;
}

/**
 * The series of `want` in a batch of `queries`. A failed call is every
 * series' error, with the data of the last call that succeeded; an error in
 * the series' own result is its error alone, without data.
 */
export function pickTrend(q: Pick<UseQueryResult<TrendResult[]>, "data" | "error" | "isPending">, queries: TrendQuery[], want: TrendQuery): TrendSlice {
  const i = queries.findIndex((x) => queryKeyOf(x) === queryKeyOf(want));
  if (i < 0) return { data: undefined, error: new Error(`${want.metric} is not in the batch`), isPending: false };
  const r = q.data?.[i];
  if (r?.error) return { data: undefined, error: new Error(r.error), isPending: false };
  return { data: r?.series, error: q.error ?? undefined, isPending: q.isPending };
}

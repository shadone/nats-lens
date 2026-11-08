// Several series over one range in one GetTimeSeriesBatch call, and the
// slice of one series in the shape a single query has, so that each chart
// shows its own data, error and loading state.
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { keepSameSystem } from "./placeholder";
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

/** Results by query identity, so that a row never takes another row's series. */
export type TrendResults = Record<string, TrendResult>;

/** Identity of a query: metric, entity and points; unset and 0 points are one query, as on the wire. */
export function trendKey(q: TrendQuery): string {
  return `${q.metric}|${q.entity?.key ?? ""}|${q.maxPoints || ""}`;
}

/** The queries without repeats, in key order, so that the same set is the same request. */
function canonical(queries: TrendQuery[]): TrendQuery[] {
  const byKey = new Map(queries.map((q) => [trendKey(q), q]));
  return [...byKey.keys()].sort().map((k) => byKey.get(k) as TrendQuery);
}

export async function getTrends(sys: string, spec: RangeSpec, queries: TrendQuery[], signal?: AbortSignal): Promise<TrendResults> {
  const qs = canonical(queries);
  const r = await lens.getTimeSeriesBatch(
    {
      ...systemInit(sys),
      range: rangeInit(spec),
      maxPoints: TREND_POINTS,
      queries: qs.map((q) => ({ metric: q.metric, entity: entityInit(q.entity), maxPoints: q.maxPoints ?? 0 })),
    },
    { signal },
  );
  if (r.results.length !== qs.length) throw new Error(`the server answered ${r.results.length} of ${qs.length} series`);
  const out: TrendResults = {};
  qs.forEach((q, i) => {
    const x = r.results[i];
    out[trendKey(q)] = x.error ? { error: x.error } : { series: trendSeries(x.series) };
  });
  return out;
}

/** The queries' series over the range, in one call, by trendKey. */
export function useTrends(spec: RangeSpec, queries: TrendQuery[]) {
  const sys = useSystem();
  const qs = canonical(queries);
  return useQuery({
    queryKey: ["trends", sys, rangeKey(spec), qs.map(trendKey)],
    queryFn: ({ signal }) => getTrends(sys, spec, qs, signal),
    refetchInterval: spec.kind === "live" ? LIVE_REFRESH_MS : false,
    placeholderData: keepSameSystem(sys),
    enabled: qs.length > 0,
  });
}

/** One series of a batch, shaped like a single query's result. */
export interface TrendSlice {
  /** Requested identity remains known while samples are missing or loading. */
  metric?: string;
  data: TrendSeries | undefined;
  error: unknown;
  isPending: boolean;
}

/**
 * The series of `want` in a batch of `queries`, found by its key. A failed
 * call is every series' error, with the data of the last call that
 * succeeded; an error in the series' own result is its error alone,
 * without data. A series the data does not hold yet, such as a row that
 * just appeared while the previous answer is shown, is pending without
 * data, or has the call's error once it failed.
 */
export function pickTrend(q: Pick<UseQueryResult<TrendResults>, "data" | "error" | "isPending">, queries: TrendQuery[], want: TrendQuery): TrendSlice {
  const key = trendKey(want);
  if (!queries.some((x) => trendKey(x) === key)) return { metric: want.metric, data: undefined, error: new Error(`${want.metric} is not in the batch`), isPending: false };
  const err = q.error ?? undefined;
  const r = q.data?.[key];
  if (!r) return { metric: want.metric, data: undefined, error: err, isPending: !err };
  if (r.error) return { metric: want.metric, data: undefined, error: new Error(r.error), isPending: false };
  return { metric: want.metric, data: r.series, error: err, isPending: q.isPending };
}

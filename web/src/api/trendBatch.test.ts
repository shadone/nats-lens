import { create } from "@bufbuild/protobuf";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { EntityType } from "../gen/natslens/v1/common_pb";
import { GetTimeSeriesBatchResponseSchema, SeriesSchema } from "../gen/natslens/v1/timeseries_pb";
import { lens } from "./client";
import type { Entity } from "./model";
import type { TrendSeries } from "./overview";
import { getTrends, pickTrend, trendKey, type TrendQuery, type TrendResults } from "./trendBatch";

vi.mock("./client", () => ({ lens: { getTimeSeriesBatch: vi.fn() } }));
const batch = vi.mocked(lens.getTimeSeriesBatch);

const live = { kind: "live" as const, preset: "24h" as const };
const consumer: Entity = { kind: "consumer", key: "APP/order-jobs/resize-worker", name: "resize-worker", account: "APP", stream: "order-jobs", consumer: "resize-worker", server: "" };

beforeEach(() => vi.resetAllMocks());

describe("getTrends", () => {
  it("sends the system, the range once, the default points and each query", async () => {
    batch.mockImplementation(async (req) => create(GetTimeSeriesBatchResponseSchema, { results: (req.queries ?? []).map(() => ({})) }));
    await getTrends("example-global", live, [{ metric: "cluster.connections" }, { metric: "consumer.delivered_rate", entity: consumer, maxPoints: 24 }]);
    const req = batch.mock.calls[0][0] as Record<string, unknown> & { queries: Record<string, unknown>[] };
    expect(req).toMatchObject({ system: "example-global", maxPoints: 96 });
    expect(req.range).toBeDefined();
    expect(req.queries[0]).toMatchObject({ metric: "cluster.connections", maxPoints: 0 });
    expect(req.queries[0].entity).toBeUndefined();
    expect(req.queries[1]).toMatchObject({ metric: "consumer.delivered_rate", maxPoints: 24 });
    expect(req.queries[1].entity).toMatchObject({ type: EntityType.CONSUMER, key: consumer.key, account: "APP" });
  });

  it("sends the queries once each in key order, so the same set is the same request", async () => {
    batch.mockImplementation(async (req) => create(GetTimeSeriesBatchResponseSchema, { results: (req.queries ?? []).map(() => ({})) }));
    await getTrends("", live, [{ metric: "cluster.servers_up" }, { metric: "cluster.connections" }, { metric: "cluster.servers_up" }]);
    const req = batch.mock.calls[0][0] as { queries: { metric: string }[] };
    expect(req.queries.map((q) => q.metric)).toEqual(["cluster.connections", "cluster.servers_up"]);
  });

  it("keys the results by query and passes a per-query error through", async () => {
    batch.mockResolvedValue(
      create(GetTimeSeriesBatchResponseSchema, {
        results: [{ series: create(SeriesSchema, { metric: "cluster.connections" }) }, { error: "invalid argument: unknown metric" }],
      }),
    );
    const r = await getTrends("", live, [{ metric: "cluster.connections" }, { metric: "cluster.nope" }]);
    expect(r["cluster.connections||"].series?.metric).toBe("cluster.connections");
    expect(r["cluster.nope||"]).toEqual({ error: "invalid argument: unknown metric" });
    expect(batch.mock.calls[0][0]).not.toHaveProperty("system");
  });
});

describe("pickTrend", () => {
  const queries: TrendQuery[] = [{ metric: "cluster.connections" }, { metric: "cluster.slow_consumers", maxPoints: 24 }, { metric: "consumer.delivered_rate", entity: consumer }];
  const s = (metric: string) => ({ metric }) as TrendSeries;
  const data: TrendResults = {
    [trendKey(queries[0])]: { series: s("cluster.connections") },
    [trendKey(queries[1])]: { error: "not found" },
    [trendKey(queries[2])]: { series: s("consumer.delivered_rate") },
  };

  it("gives each query its own series", () => {
    const q = { data, error: null, isPending: false };
    expect(pickTrend(q, queries, { metric: "cluster.connections" })).toEqual({ metric: "cluster.connections", data: s("cluster.connections"), error: undefined, isPending: false });
    expect(pickTrend(q, queries, { metric: "consumer.delivered_rate", entity: consumer }).data).toEqual(s("consumer.delivered_rate"));
  });

  it("shows a query's own error on that query only", () => {
    const q = { data, error: null, isPending: false };
    const bad = pickTrend(q, queries, { metric: "cluster.slow_consumers", maxPoints: 24 });
    expect(bad.data).toBeUndefined();
    expect((bad.error as Error).message).toBe("not found");
    expect(pickTrend(q, queries, { metric: "cluster.connections" }).error).toBeUndefined();
  });

  it("matches the entity and the points, not only the metric", () => {
    const q = { data, error: null, isPending: false };
    expect((pickTrend(q, queries, { metric: "cluster.slow_consumers" }).error as Error).message).toMatch(/not in the batch/);
    expect((pickTrend(q, queries, { metric: "consumer.delivered_rate" }).error as Error).message).toMatch(/not in the batch/);
  });

  it("a failed call is every query's error, with the data it had", () => {
    const err = new Error("unavailable");
    const q = { data, error: err, isPending: false };
    expect(pickTrend(q, queries, { metric: "cluster.connections" })).toEqual({ metric: "cluster.connections", data: s("cluster.connections"), error: err, isPending: false });
    expect(pickTrend({ data: undefined, error: err, isPending: false }, queries, { metric: "cluster.connections" })).toEqual({ metric: "cluster.connections", data: undefined, error: err, isPending: false });
  });

  it("a query the data does not hold yet is pending without data, or has the call's error", () => {
    const other: TrendQuery = { metric: "consumer.delivered_rate", entity: { ...consumer, key: "APP/order-jobs/thumb-worker", consumer: "thumb-worker" } };
    const qs = [...queries, other];
    expect(pickTrend({ data, error: null, isPending: false }, qs, other)).toEqual({ metric: other.metric, data: undefined, error: undefined, isPending: true });
    const err = new Error("unavailable");
    expect(pickTrend({ data, error: err, isPending: false }, qs, other)).toEqual({ metric: other.metric, data: undefined, error: err, isPending: false });
  });

  it("is pending while the call is", () => {
    expect(pickTrend({ data: undefined, error: null, isPending: true }, queries, { metric: "cluster.connections" }).isPending).toBe(true);
  });
});

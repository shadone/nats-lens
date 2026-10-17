import { beforeEach, describe, expect, it, vi } from "vitest";
import { EntityType } from "../gen/natslens/v1/common_pb";
import { ConsumerSort } from "../gen/natslens/v1/consumers_pb";
import { lens } from "./client";
import type { Entity } from "./model";
import { getTrend, listBusiestConsumers, listStreamPlacement } from "./overview";

vi.mock("./client", () => ({
  lens: {
    getTimeSeries: vi.fn(async () => ({ series: undefined })),
    listConsumers: vi.fn(async () => ({ consumers: [] })),
    listStreams: vi.fn(async () => ({ streams: [], totals: undefined })),
  },
}));

const live = { kind: "live" as const, preset: "24h" as const };
const consumer: Entity = { kind: "consumer", key: "APP/order-jobs/resize-worker", name: "resize-worker", account: "APP", stream: "order-jobs", consumer: "resize-worker", server: "" };

beforeEach(() => vi.clearAllMocks());

describe("overview requests", () => {
  it("getTimeSeries carries the system, the metric and the entity", async () => {
    await getTrend("example-global", live, "consumer.delivered_rate", consumer);
    const req = vi.mocked(lens.getTimeSeries).mock.calls[0][0] as Record<string, unknown>;
    expect(req).toMatchObject({ system: "example-global", metric: "consumer.delivered_rate", maxPoints: 96 });
    expect(req.entity).toMatchObject({ type: EntityType.CONSUMER, key: consumer.key, account: "APP", stream: "order-jobs", consumer: "resize-worker" });
  });

  it("getTimeSeries of a cluster metric sends no entity", async () => {
    await getTrend("example-prod", live, "cluster.connections", undefined);
    const req = vi.mocked(lens.getTimeSeries).mock.calls[0][0] as Record<string, unknown>;
    expect(req).toMatchObject({ system: "example-prod", metric: "cluster.connections" });
    expect(req.entity).toBeUndefined();
  });

  it("listConsumers carries the system, the account, the delivered-rate sort and the limit", async () => {
    await listBusiestConsumers("example-global", live, "APP", 6);
    expect(vi.mocked(lens.listConsumers).mock.calls[0][0]).toMatchObject({ system: "example-global", account: "APP", sort: ConsumerSort.DELIVERED_RATE, limit: 6 });
  });

  it("listStreams carries the system, the account and the limit, and reads the count of all streams", async () => {
    vi.mocked(lens.listStreams).mockResolvedValueOnce({ streams: [], totals: undefined, totalCount: 18 } as never);
    const r = await listStreamPlacement("example-global", live, "APP");
    expect(vi.mocked(lens.listStreams).mock.calls[0][0]).toMatchObject({ system: "example-global", account: "APP", limit: 10 });
    expect(r.count).toBe(18);
  });

  it("an empty system is left out, so the server reads its default", async () => {
    await listStreamPlacement("", live, "");
    expect(vi.mocked(lens.listStreams).mock.calls[0][0]).not.toHaveProperty("system");
  });
});

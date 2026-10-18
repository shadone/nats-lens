import { beforeEach, describe, expect, it, vi } from "vitest";
import { ConsumerSort } from "../gen/natslens/v1/consumers_pb";
import { lens } from "./client";
import { listBusiestConsumers, listStreamPlacement } from "./overview";

vi.mock("./client", () => ({
  lens: {
    listConsumers: vi.fn(async () => ({ consumers: [] })),
    listStreams: vi.fn(async () => ({ streams: [], totals: undefined })),
  },
}));

const live = { kind: "live" as const, preset: "24h" as const };

beforeEach(() => vi.clearAllMocks());

describe("overview requests", () => {
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

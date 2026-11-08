import { describe, expect, it, vi } from "vitest";
import { lens } from "./client";
import { listConsumers, listStreams } from "./requests";

vi.mock("./client", () => ({ lens: { listStreams: vi.fn(), listConsumers: vi.fn() } }));
const spec = { kind: "live", preset: "24h" } as never;

describe("Bounded entity list requests", () => {
  it("traverses filter/search reads with the pinned range and preserves all-match totals", async () => {
    const range = { start: { seconds: 1n, nanos: 0 }, end: { seconds: 2n, nanos: 0 } };
    vi.mocked(lens.listStreams).mockResolvedValueOnce({ streams: [], nextPageToken: "next", totalCount: 200, totals: { bytes: 999n }, range } as never)
      .mockResolvedValueOnce({ streams: [], nextPageToken: "", totalCount: 200, totals: { bytes: 999n }, range } as never);
    const r = await listStreams("example-prod", spec, false);
    expect(r.totalCount).toBe(200);
    expect(r.totals.bytes).toBe(999);
    expect(vi.mocked(lens.listStreams).mock.calls[1][0]).toMatchObject({ range, pageToken: "next", limit: 100, summaryOnly: true });
  });
  it("reads only one UI page and lets the token supply the frozen range", async () => {
    vi.mocked(lens.listConsumers).mockResolvedValueOnce({ consumers: [], nextPageToken: "later", totalCount: 1200 } as never);
    const r = await listConsumers("example-prod", spec, { account: "APP" }, undefined, { pageToken: "next" });
    expect(r.totalCount).toBe(1200);
    expect(r.nextPageToken).toBe("later");
    expect(vi.mocked(lens.listConsumers).mock.calls[0][0]).toMatchObject({ range: undefined, account: "APP", pageToken: "next", limit: 100 });
  });
  it("cancels traversal between pages", async () => {
    const controller = new AbortController();
    vi.mocked(lens.listStreams).mockImplementationOnce(async () => { controller.abort(); return { streams: [], nextPageToken: "next" } as never; });
    await expect(listStreams("example-prod", spec, false, controller.signal)).rejects.toThrow();
  });
});

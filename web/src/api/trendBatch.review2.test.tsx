// Cases from the second review of the keyed trend batch: placeholder,
// range switch, dedupe by max_points, the stale flag, a short answer.
import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GetTimeSeriesBatchResponseSchema, type GetTimeSeriesBatchRequestSchema, type GetTimeSeriesBatchResponse } from "../gen/natslens/v1/timeseries_pb";
import { lens } from "./client";
import type { Entity } from "./model";
import { pickTrend, useTrends, type TrendQuery } from "./trendBatch";
import type { RangeSpec } from "../lib/timeRange";

vi.mock("./client", () => ({ lens: { getTimeSeriesBatch: vi.fn() } }));
const batch = vi.mocked(lens.getTimeSeriesBatch);

const ent = (n: string): Entity => ({ kind: "consumer", key: `APP/s/${n}`, name: n, account: "APP", stream: "s", consumer: n, server: "" });
const [A, B, C] = [ent("a"), ent("b"), ent("c")];
const VALUE: Record<string, number> = { a: 1, b: 2, c: 3 };
const M = "consumer.delivered_rate";
const day = { kind: "live" as const, preset: "24h" as const } as RangeSpec;
const hour = { kind: "live" as const, preset: "1h" as const } as RangeSpec;

type Req = MessageInitShape<typeof GetTimeSeriesBatchRequestSchema>;
/** Each query's one point is its consumer's value plus its max_points, times the generation. */
function answerer(gen = 1) {
  return async (req: Req): Promise<GetTimeSeriesBatchResponse> =>
    create(GetTimeSeriesBatchResponseSchema, {
      results: (req.queries ?? []).map((q) => ({ series: { metric: M, points: [{ value: gen * (VALUE[q.entity?.consumer ?? ""] + (q.maxPoints ?? 0)) }] } })),
    });
}

function wrap() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

function render(queries: TrendQuery[], spec: RangeSpec = day) {
  return renderHook(
    ({ queries, spec }: { queries: TrendQuery[]; spec: RangeSpec }) => {
      const b = useTrends(spec, queries);
      return { b, pick: (w: TrendQuery) => pickTrend(b, queries, w) };
    },
    { wrapper: wrap(), initialProps: { queries, spec } },
  );
}

const q = (e: Entity, maxPoints?: number): TrendQuery => ({ metric: M, entity: e, maxPoints });

beforeEach(() => vi.resetAllMocks());

describe("review: keyed trend batch", () => {
  it("a new row is pending under the placeholder, then gets its own value; the old row keeps its own", async () => {
    batch.mockImplementationOnce(answerer());
    let resolve: (r: GetTimeSeriesBatchResponse) => void = () => {};
    batch.mockImplementationOnce(() => new Promise((res) => (resolve = (r) => res(r))) as never);
    const { result, rerender } = render([q(A), q(B)]);
    await waitFor(() => expect(result.current.pick(q(B)).data?.points[0].value).toBe(2));
    rerender({ queries: [q(A), q(C)], spec: day });
    expect(result.current.pick(q(C))).toMatchObject({ data: undefined, isPending: true, error: undefined });
    expect(result.current.pick(q(A)).data?.points[0].value).toBe(1);
    const req = batch.mock.calls[1][0] as Req;
    await act(async () => resolve(await answerer(10)(req)));
    await waitFor(() => expect(result.current.pick(q(C)).data?.points[0].value).toBe(30));
    expect(result.current.pick(q(A)).data?.points[0].value).toBe(10);
  });

  it("a range switch keeps showing each row's own series until the new range answers", async () => {
    batch.mockImplementationOnce(answerer());
    let resolve: (r: GetTimeSeriesBatchResponse) => void = () => {};
    batch.mockImplementationOnce(() => new Promise((res) => (resolve = res)) as never);
    const { result, rerender } = render([q(A), q(B)]);
    await waitFor(() => expect(result.current.pick(q(B)).data?.points[0].value).toBe(2));
    rerender({ queries: [q(A), q(B)], spec: hour });
    expect(result.current.b.isPlaceholderData).toBe(true);
    expect(result.current.pick(q(A)).data?.points[0].value).toBe(1);
    expect(result.current.pick(q(B)).data?.points[0].value).toBe(2);
    const req = batch.mock.calls[1][0] as Req;
    await act(async () => resolve(await answerer(10)(req)));
    await waitFor(() => expect(result.current.pick(q(B)).data?.points[0].value).toBe(20));
  });

  it("the same series at two max_points is two queries, each picked by its own points; an exact repeat is one", async () => {
    batch.mockImplementation(answerer());
    const { result } = render([q(A), q(A, 24), q(A, 24)]);
    await waitFor(() => expect(result.current.pick(q(A, 24)).data?.points[0].value).toBe(25));
    expect(result.current.pick(q(A)).data?.points[0].value).toBe(1);
    expect((batch.mock.calls[0][0] as Req).queries).toHaveLength(2);
  });

  it("maxPoints undefined and 0 are one query, sent once", async () => {
    batch.mockImplementation(answerer());
    const { result } = render([q(A), q(A, 0)]);
    await waitFor(() => expect(result.current.pick(q(A, 0)).data).toBeDefined());
    expect(result.current.pick(q(A)).data).toEqual(result.current.pick(q(A, 0)).data);
    expect((batch.mock.calls[0][0] as Req).queries).toHaveLength(1);
  });

  it("a failed refresh of the same key keeps the data and the error, so the page is stale", async () => {
    batch.mockImplementationOnce(answerer());
    batch.mockRejectedValueOnce(new Error("unavailable"));
    const { result } = render([q(A), q(B)]);
    await waitFor(() => expect(result.current.pick(q(A)).data).toBeDefined());
    await act(async () => {
      await result.current.b.refetch();
    });
    await waitFor(() => expect(result.current.b.error).toBeTruthy());
    const b = result.current.b;
    // useReportStale condition
    expect(b.data !== undefined && !!b.error).toBe(true);
    expect(result.current.pick(q(A))).toMatchObject({ error: b.error });
    expect(result.current.pick(q(A)).data?.points[0].value).toBe(1);
  });

  it("an answer with fewer results than queries fails the call, so no chart stays pending", async () => {
    batch.mockResolvedValue(create(GetTimeSeriesBatchResponseSchema, { results: [{ series: { metric: M, points: [{ value: 1 }] } }] }));
    const { result } = render([q(A), q(B)]);
    await waitFor(() => expect(result.current.b.isError).toBe(true));
    expect(result.current.pick(q(B))).toMatchObject({ data: undefined, isPending: false });
    expect(result.current.pick(q(B)).error).toBeTruthy();
    expect(result.current.pick(q(A)).error).toBeTruthy();
  });
});

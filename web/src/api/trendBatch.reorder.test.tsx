// Sparks of a changing set of rows: each row keeps its own series, never
// another row's, while a new batch loads and after it fails. Adapted from
// the review of the batch RPC.
import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { GetTimeSeriesBatchResponseSchema, type GetTimeSeriesBatchRequestSchema, type GetTimeSeriesBatchResponse } from "../gen/natslens/v1/timeseries_pb";
import { lens } from "./client";
import type { Entity } from "./model";
import { pickTrend, useTrends, type TrendQuery } from "./trendBatch";

vi.mock("./client", () => ({ lens: { getTimeSeriesBatch: vi.fn() } }));
const batch = vi.mocked(lens.getTimeSeriesBatch);

const ent = (n: string): Entity => ({ kind: "consumer", key: `APP/s/${n}`, name: n, account: "APP", stream: "s", consumer: n, server: "" });
const [A, B, C] = [ent("a"), ent("b"), ent("c")];
const VALUE: Record<string, number> = { a: 1, b: 2, c: 3 };
const live = { kind: "live" as const, preset: "24h" as const };
const M = "consumer.delivered_rate";

/** Answers each query with a series whose one point is its consumer's value. */
async function answer(req: MessageInitShape<typeof GetTimeSeriesBatchRequestSchema>): Promise<GetTimeSeriesBatchResponse> {
  return create(GetTimeSeriesBatchResponseSchema, {
    results: (req.queries ?? []).map((q) => ({ series: { metric: M, points: [{ value: VALUE[q.entity?.consumer ?? ""] }] } })),
  });
}

function render(rows: Entity[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  );
  return renderHook(
    ({ rows }: { rows: Entity[] }) => {
      const queries: TrendQuery[] = rows.map((e) => ({ metric: M, entity: e }));
      const b = useTrends(live, queries);
      const pick = (e: Entity) => pickTrend(b, queries, { metric: M, entity: e });
      return { a: pick(A), b: pick(B), c: pick(C) };
    },
    { wrapper, initialProps: { rows } },
  );
}

beforeEach(() => vi.resetAllMocks());

describe("sparks when the rows change", () => {
  it("a new row shows no data, not another row's line, while it loads and after the call fails", async () => {
    batch.mockImplementationOnce(answer);
    let reject: (e: Error) => void = () => {};
    batch.mockImplementationOnce(() => new Promise<never>((_, rej) => (reject = rej)));
    const { result, rerender } = render([A, B]);
    await waitFor(() => expect(result.current.a.data?.points[0].value).toBe(1));
    rerender({ rows: [C, A] });
    expect(result.current.c.data).toBeUndefined();
    expect(result.current.c.isPending).toBe(true);
    expect(result.current.a.data?.points[0].value).toBe(1);
    reject(new Error("unavailable"));
    await waitFor(() => expect(result.current.c.error).toBeTruthy());
    expect(result.current.c.data).toBeUndefined();
    // A failed call for a new key drops the shown answer; a must not show another line.
    expect(result.current.a.data?.points[0].value ?? 1).toBe(1);
  });

  it("a pure reorder keeps each row's series and sends no new call", async () => {
    batch.mockImplementation(answer);
    const { result, rerender } = render([A, B]);
    await waitFor(() => expect(result.current.b.data?.points[0].value).toBe(2));
    rerender({ rows: [B, A] });
    expect(result.current.a.data?.points[0].value).toBe(1);
    expect(result.current.b.data?.points[0].value).toBe(2);
    expect(batch).toHaveBeenCalledTimes(1);
  });
});

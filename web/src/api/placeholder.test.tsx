// A system-scoped hook keeps its previous answer while the next key loads
// only for the same system: a range change keeps it, a system switch does not.
import { create } from "@bufbuild/protobuf";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useNavigate } from "react-router";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ListKeyValueStoresResponseSchema } from "../gen/natslens/v1/buckets_pb";
import { ListEventsResponseSchema } from "../gen/natslens/v1/events_pb";
import { ListFindingsResponseSchema } from "../gen/natslens/v1/findings_pb";
import { GetOverviewResponseSchema } from "../gen/natslens/v1/overview_pb";
import { GetPlacementResponseSchema } from "../gen/natslens/v1/placement_pb";
import { ListServersResponseSchema } from "../gen/natslens/v1/servers_pb";
import { GetTimeSeriesBatchResponseSchema } from "../gen/natslens/v1/timeseries_pb";
import type { RangeSpec } from "../lib/timeRange";
import { useKeyValueStores } from "./buckets";
import { lens } from "./client";
import { useEventFeed } from "./events";
import { useCurrentFindings } from "./overview";
import { keepSameSystem } from "./placeholder";
import { useOverview, usePlacement } from "./queries";
import { useServerList } from "./servers";
import { useTrends } from "./trendBatch";

vi.mock("./client", () => ({
  lens: {
    getOverview: vi.fn(),
    getPlacement: vi.fn(),
    listKeyValueStores: vi.fn(),
    listFindings: vi.fn(),
    listEvents: vi.fn(),
    listServers: vi.fn(),
    getTimeSeriesBatch: vi.fn(),
  },
}));

const day = { kind: "live", preset: "24h" } as RangeSpec;
const hour = { kind: "live", preset: "1h" } as RangeSpec;

describe("keepSameSystem", () => {
  const prev = { rows: 1 };
  it("keeps the previous answer of the same system", () => {
    expect(keepSameSystem("shop-prod")(prev, { queryKey: ["overview", "shop-prod", "live:24h"] })).toBe(prev);
  });
  it("drops another system's answer", () => {
    expect(keepSameSystem("shop-staging")(prev, { queryKey: ["overview", "shop-prod", "live:24h"] })).toBeUndefined();
  });
  it("tells the default system apart from a named one", () => {
    expect(keepSameSystem("shop-prod")(prev, { queryKey: ["overview", "", "live:24h"] })).toBeUndefined();
  });
  it("drops an answer whose other named positions differ", () => {
    const keep = keepSameSystem("shop-prod", { 3: "APP" });
    expect(keep(prev, { queryKey: ["subjects", "shop-prod", "live:24h", "APP"] })).toBe(prev);
    expect(keep(prev, { queryKey: ["subjects", "shop-prod", "live:24h", "SYS"] })).toBeUndefined();
  });
  it("keeps nothing without a previous query", () => {
    expect(keepSameSystem("shop-prod")(prev, undefined)).toBeUndefined();
  });
});

/** Set to hold every call for the staging system until the test ends. */
let holdStaging = false;

function answer<T>(make: () => T) {
  return (async (req: { system?: string }) => {
    if (holdStaging && req.system === "shop-staging") await new Promise(() => {});
    return make();
  }) as never;
}

beforeEach(() => {
  vi.clearAllMocks();
  holdStaging = false;
  const l = vi.mocked(lens);
  l.getOverview.mockImplementation(answer(() => create(GetOverviewResponseSchema)));
  l.getPlacement.mockImplementation(answer(() => create(GetPlacementResponseSchema)));
  l.listKeyValueStores.mockImplementation(answer(() => create(ListKeyValueStoresResponseSchema)));
  l.listFindings.mockImplementation(answer(() => create(ListFindingsResponseSchema)));
  l.listEvents.mockImplementation(answer(() => create(ListEventsResponseSchema)));
  l.listServers.mockImplementation(answer(() => create(ListServersResponseSchema)));
  l.getTimeSeriesBatch.mockImplementation(answer(() => create(GetTimeSeriesBatchResponseSchema, { results: [{ series: { metric: "cluster.connections" } }] })));
});

const feed = { sources: [], kinds: [], clientName: "", worthReading: true, pageSize: 50, pageToken: "" };
const trend = [{ metric: "cluster.connections", entity: undefined }];

/** One representative system-scoped hook of each file that keeps answers. */
const HOOKS: [string, (spec: RangeSpec) => { data: unknown; isPlaceholderData: boolean }][] = [
  ["useOverview", (spec) => useOverview(spec)],
  ["usePlacement", (spec) => usePlacement(spec, "all")],
  ["useKeyValueStores", (spec) => useKeyValueStores(spec, "")],
  ["useCurrentFindings", (spec) => useCurrentFindings(spec)],
  ["useEventFeed", (spec) => useEventFeed(spec, feed)],
  ["useServerList", (spec) => useServerList(spec)],
  ["useTrends", (spec) => useTrends(spec, trend)],
];

function renderOn(hook: (spec: RangeSpec) => { data: unknown; isPlaceholderData: boolean }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/?system=shop-prod"]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
  return renderHook(({ spec }: { spec: RangeSpec }) => ({ q: hook(spec), navigate: useNavigate() }), { wrapper, initialProps: { spec: day } });
}

describe.each(HOOKS)("%s while the next key loads", (_, hook) => {
  it("shows nothing of the previous system after a switch", async () => {
    holdStaging = true;
    const { result } = renderOn(hook);
    await waitFor(() => expect(result.current.q.data).toBeDefined());
    act(() => result.current.navigate("/?system=shop-staging"));
    await waitFor(() => expect(result.current.q.data).toBeUndefined());
    expect(result.current.q.isPlaceholderData).toBe(false);
  });

  it("keeps the previous range's answer of the same system", async () => {
    const { result, rerender } = renderOn(hook);
    await waitFor(() => expect(result.current.q.data).toBeDefined());
    for (const m of Object.values(vi.mocked(lens))) m.mockImplementation((() => new Promise(() => {})) as never);
    rerender({ spec: hour });
    expect(result.current.q.isPlaceholderData).toBe(true);
    expect(result.current.q.data).toBeDefined();
  });
});

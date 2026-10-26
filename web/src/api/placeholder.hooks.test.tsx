// Every system-scoped hook of web/src/api: a switch of system drops the
// previous answer, a range change keeps it, and an account-scoped hook
// drops it on a change of account. The lens never answers, so only the
// seeded answers are ever shown.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter, useNavigate } from "react-router";
import { describe, expect, it, vi } from "vitest";
import type { RangeSpec } from "../lib/timeRange";
import * as buckets from "./buckets";
import * as events from "./events";
import * as overview from "./overview";
import * as q from "./queries";
import * as replay from "./replay";
import * as servers from "./servers";
import * as trend from "./trendBatch";

vi.mock("./client", () => ({ lens: new Proxy({}, { get: () => () => new Promise(() => {}) }) }));

const day = { kind: "live", preset: "24h" } as RangeSpec;
const hour = { kind: "live", preset: "1h" } as RangeSpec;
const A = {} as never;

type H = (spec: RangeSpec, account: string) => { data: unknown; isPlaceholderData: boolean };
const HOOKS: [string, H, boolean][] = [
  ["useOverview", (s) => q.useOverview(s), false],
  ["useFindings", (s) => q.useFindings(s, A), false],
  ["useMutes", () => q.useMutes(false), false],
  ["useTimeline", (s) => q.useTimeline(s), false],
  ["useEvents", (s) => q.useEvents(s, A), false],
  ["useStreams", (s) => q.useStreams(s), false],
  ["useStreamNames", (s) => q.useStreamNames(s), false],
  ["useConsumers", (s) => q.useConsumers(s, {}), false],
  ["usePlacement", (s) => q.usePlacement(s, "all"), false],
  ["usePlacement keepFor", (s) => q.usePlacement(s, "all", true, "k"), false],
  ["useTopology", (s) => q.useTopology(s), false],
  ["useAccountsAndAuth", (s) => q.useAccountsAndAuth(s), false],
  ["useCapacity", (s) => q.useCapacity(s), false],
  ["useSubjects", (s, a) => q.useSubjects(s, a, A), true],
  ["useMicroServices", (s, a) => q.useMicroServices(s, a), true],
  ["useApplications", (s, a) => q.useApplications(s, a, false), true],
  ["useServers", (s) => q.useServers(s), false],
  ["useConnections", (s) => q.useConnections(s, A), false],
  ["useSeries", (s) => q.useSeries(s, "m", undefined), false],
  ["useKeyValueStores", (s, a) => buckets.useKeyValueStores(s, a), true],
  ["useObjectStores", (s, a) => buckets.useObjectStores(s, a), true],
  ["useEventFeed", (s) => events.useEventFeed(s, { sources: [], kinds: [], clientName: "", worthReading: true, pageSize: 50, pageToken: "" } as never), false],
  ["useBusiestConsumers", (s, a) => overview.useBusiestConsumers(s, a), true],
  ["useStreamPlacement", (s, a) => overview.useStreamPlacement(s, a), true],
  ["useCurrentFindings", (s) => overview.useCurrentFindings(s), false],
  ["useReplay", () => replay.useReplay(1000, 2000), false],
  ["useServerList", (s) => servers.useServerList(s), false],
  ["useBandSeries", (s) => servers.useBandSeries(s, "m"), false],
  ["useServerEvents", (s) => servers.useServerEvents(s, "n1"), false],
  ["useTrends", (s) => trend.useTrends(s, [{ metric: "m" }] as never), false],
];

function setup(hook: H) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={["/?system=shop-prod"]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
  const r = renderHook(
    ({ spec, account }: { spec: RangeSpec; account: string }) => ({ q: hook(spec, account), navigate: useNavigate() }),
    { wrapper, initialProps: { spec: day, account: "APP" } },
  );
  // Seed every query in the cache with a sentinel answer.
  act(() => {
    for (const query of qc.getQueryCache().findAll()) qc.setQueryData(query.queryKey, { sentinel: true });
  });
  return { ...r, qc };
}

describe.each(HOOKS)("%s", (name, hook, hasAccount) => {
  it("seeded", async () => {
    const { result } = setup(hook);
    await waitFor(() => expect(result.current.q.data).toBeDefined());
  });
  it("drops on system switch", async () => {
    const { result } = setup(hook);
    await waitFor(() => expect(result.current.q.data).toBeDefined());
    act(() => result.current.navigate("/?system=shop-staging"));
    await waitFor(() => expect(result.current.q.data).toBeUndefined());
    expect(result.current.q.isPlaceholderData).toBe(false);
  });
  it("drops on switch to default system (empty)", async () => {
    const { result } = setup(hook);
    await waitFor(() => expect(result.current.q.data).toBeDefined());
    act(() => result.current.navigate("/"));
    await waitFor(() => expect(result.current.q.data).toBeUndefined());
  });
  it("keeps on range change in same system", async () => {
    const { result, rerender } = setup(hook);
    await waitFor(() => expect(result.current.q.data).toBeDefined());
    rerender({ spec: hour, account: "APP" });
    if (name === "useMutes" || name === "useReplay") return;
    expect(result.current.q.data).toBeDefined();
  });
  it.runIf(hasAccount)("drops on account change", async () => {
    const { result, rerender } = setup(hook);
    await waitFor(() => expect(result.current.q.data).toBeDefined());
    rerender({ spec: day, account: "SYS" });
    expect(result.current.q.data).toBeUndefined();
  });
});

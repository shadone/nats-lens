import { create } from "@bufbuild/protobuf";
import { GetDataHistoryResponseSchema } from "../gen/natslens/v1/history_pb";
// Every query hook keys on the system of the page and sends it.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { MemoryRouter, useNavigate } from "react-router";
import { lens } from "./client";
import * as q from "./queries";

vi.mock("./client", () => {
  const ok = () => vi.fn(async () => ({}));
  return {
    lens: {
      getOverview: ok(),
      listFindings: ok(),
      getFinding: ok(),
      listMutes: ok(),
      muteFinding: ok(),
      unmuteFinding: ok(),
      getStatusTimeline: ok(),
      listEvents: ok(),
      getEvent: ok(),
      listStreams: ok(),
      getStream: ok(),
      listConsumers: ok(),
      getConsumer: ok(),
      listServers: ok(),
      listConnections: ok(),
      getTimeSeries: ok(),
      getLensInfo: ok(),
      getShellStatus: ok(),
      getAccountsAndAuth: ok(),
      getDataHistory: ok(),
    },
  };
});

const live = { kind: "live", preset: "1h" } as const;
const entity = { kind: "stream", key: "APP/order-jobs", name: "order-jobs", account: "APP", stream: "order-jobs", consumer: "", server: "" } as const;

let navigate: (to: string) => void = () => {};

/** Calls every query hook of queries.ts once. */
function AllHooks() {
  navigate = useNavigate();
  q.useOverview(live);
  q.useFindings(live, {});
  q.useFinding(live, "f1");
  q.useMutes(false);
  q.useTimeline(live, entity);
  q.useEvents(live, {});
  q.useEvent("e1");
  q.useStreams(live);
  q.useStreamNames(live);
  q.useStream(live, "APP", "order-jobs");
  q.useConsumers(live, {});
  q.useConsumer(live, "APP", "order-jobs", "resize-worker");
  q.useServers(live);
  q.useConnections(live, {});
  q.useSeries(live, "cluster.connections", undefined);
  q.useLensInfo();
  q.useShellStatus();
  q.useAccountsAndAuth(live);
  q.useDataHistory(live);
  return null;
}

/** Lens methods behind the hooks above; each must have sent the system. */
const METHODS = [
  "getOverview",
  "listFindings",
  "getFinding",
  "listMutes",
  "getStatusTimeline",
  "listEvents",
  "getEvent",
  "listStreams",
  "getStream",
  "listConsumers",
  "getConsumer",
  "listServers",
  "listConnections",
  "getTimeSeries",
  "getLensInfo",
  "getShellStatus",
  "getAccountsAndAuth",
  "getDataHistory",
] as const;

function setup(route: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[route]}>
        <AllHooks />
      </MemoryRouter>
    </QueryClientProvider>,
  );
  return qc;
}

function lastSystem(m: (typeof METHODS)[number]): unknown {
  const calls = vi.mocked(lens[m]).mock.calls;
  return (calls.at(-1)?.[0] as { system?: string } | undefined)?.system;
}

describe("query keys and the system", () => {
  afterEach(() => vi.clearAllMocks());

  it("puts the system into every query key and every request", async () => {
    const qc = setup("/?system=sys-a");
    await waitFor(() => METHODS.forEach((m) => expect(lens[m]).toHaveBeenCalled()));
    const keys = qc.getQueryCache().getAll().map((x) => x.queryKey);
    expect(keys.length).toBe(20);
    for (const k of keys) expect(k, JSON.stringify(k)).toContain("sys-a");
    for (const m of METHODS) expect(lastSystem(m), m).toBe("sys-a");
  });

  it("asks again with the new system when the page switches system", async () => {
    const qc = setup("/?system=sys-a");
    await waitFor(() => METHODS.forEach((m) => expect(lens[m]).toHaveBeenCalled()));
    vi.clearAllMocks();
    act(() => navigate("/?system=sys-b"));
    await waitFor(() => METHODS.forEach((m) => expect(lastSystem(m), m).toBe("sys-b")));
    const keys = qc.getQueryCache().getAll().map((x) => x.queryKey);
    expect(keys.filter((k) => k.includes("sys-b")).length).toBe(20);
  });

  it("keys a page without ?system= on the empty system and sends none", async () => {
    const qc = setup("/");
    await waitFor(() => expect(lens.listServers).toHaveBeenCalled());
    expect(lens.listServers).toHaveBeenCalledWith(expect.not.objectContaining({ system: expect.anything() }), expect.anything());
    const k = qc.getQueryCache().getAll().find((x) => x.queryKey[0] === "servers")!.queryKey;
    expect(k[1]).toBe("");
  });
});

describe("mutations and the system", () => {
  afterEach(() => vi.clearAllMocks());

  function Mutations({ onReady }: { onReady: (m: { mute: ReturnType<typeof q.useMuteFinding>; unmute: ReturnType<typeof q.useUnmuteFinding> }) => void }) {
    onReady({ mute: q.useMuteFinding(), unmute: q.useUnmuteFinding() });
    return null;
  }

  it("mutes and unmutes on the system it is given", async () => {
    const qc = new QueryClient();
    let m: Parameters<Parameters<typeof Mutations>[0]["onReady"]>[0] | undefined;
    render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/findings?system=sys-other"]}>
          <Mutations onReady={(x) => (m = x)} />
        </MemoryRouter>
      </QueryClientProvider>,
    );
    await act(async () => {
      await m!.mute.mutateAsync({ system: "sys-c", checkCode: "stream-005", reason: "r", expiresAt: Date.now() + 3_600_000, mutedBy: "op" });
      await m!.unmute.mutateAsync({ system: "sys-c", muteId: "m1", unmutedBy: "op" });
    });
    expect(vi.mocked(lens.muteFinding).mock.calls[0][0]).toMatchObject({ system: "sys-c" });
    expect(vi.mocked(lens.unmuteFinding).mock.calls[0][0]).toMatchObject({ system: "sys-c" });
  });
});

describe("live refetch", () => {
  afterEach(() => vi.clearAllMocks());

  it("refetches the Accounts page on the collector's interval in a live range, as Topology", async () => {
    const qc = setup("/?system=sys-a");
    await waitFor(() => expect(lens.getAccountsAndAuth).toHaveBeenCalled());
    const query = qc.getQueryCache().find({ queryKey: ["accounts-and-auth", "sys-a"], exact: false });
    expect(query?.observers[0]?.options.refetchInterval).toBe(30_000);
  });

  it("refetches the Data history page every 30 seconds in a live range", async () => {
    const qc = setup("/?system=sys-a");
    await waitFor(() => expect(lens.getDataHistory).toHaveBeenCalled());
    const query = qc.getQueryCache().find({ queryKey: ["data-history", "sys-a"], exact: false });
    expect(query?.observers[0]?.options.refetchInterval).toBe(30_000);
  });
});

it("shares a settled history answer with charts mounted later on the same page", async () => {
  vi.mocked(lens.getDataHistory).mockReset();
  vi.mocked(lens.getDataHistory).mockResolvedValue(create(GetDataHistoryResponseSchema));
  function Chart() { q.useDataHistory(live); return null; }
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Frame({ second }: { second: boolean }) {
    return <QueryClientProvider client={qc}><MemoryRouter><Chart />{second && <Chart />}</MemoryRouter></QueryClientProvider>;
  }
  const r = render(<Frame second={false} />);
  await waitFor(() => expect(qc.isFetching()).toBe(0));
  expect(lens.getDataHistory).toHaveBeenCalledTimes(1);
  r.rerender(<Frame second />);
  await act(async () => { await new Promise((resolve) => setTimeout(resolve, 25)); });
  expect(lens.getDataHistory).toHaveBeenCalledTimes(1);
});

it("does not lend history facts from a previous range to a pending or failing range", async () => {
  const read = vi.mocked(lens.getDataHistory);
  read.mockReset();
  read.mockResolvedValueOnce(create(GetDataHistoryResponseSchema));
  let reject!: (error: Error) => void;
  read.mockImplementationOnce(() => new Promise((_yes, no) => { reject = no; }));
  let result: ReturnType<typeof q.useDataHistory>;
  function Probe({ spec }: { spec: import("../lib/timeRange").RangeSpec }) { result = q.useDataHistory(spec); return null; }
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  function Frame({ spec }: { spec: import("../lib/timeRange").RangeSpec }) {
    return <QueryClientProvider client={qc}><MemoryRouter><Probe spec={spec} /></MemoryRouter></QueryClientProvider>;
  }
  const r = render(<Frame spec={live} />);
  await waitFor(() => expect(result!.data).toBeDefined());
  r.rerender(<Frame spec={{ kind: "live", preset: "24h" }} />);
  expect(result!.data).toBeUndefined();
  await act(async () => reject(new Error("unavailable")));
  await waitFor(() => expect(result!.isError).toBe(true));
  expect(result!.data).toBeUndefined();
});

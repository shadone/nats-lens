import { create } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Status } from "../gen/natslens/v1/common_pb";
import { ServerSummarySchema } from "../gen/natslens/v1/servers_pb";
import { CollectionState, GetShellStatusResponseSchema } from "../gen/natslens/v1/systems_pb";
import { lens } from "./client";
import { getLastData, getShellStatus, getLensInfo, listServers, muteFinding, unmuteFinding } from "./requests";

vi.mock("./client", () => ({
  lens: {
    listServers: vi.fn(async () => ({ servers: [], expected: [], missing: [] })),
    getLensInfo: vi.fn(async () => ({})),
    muteFinding: vi.fn(async () => ({})),
    unmuteFinding: vi.fn(async () => ({})),
    getShellStatus: vi.fn(),
  },
}));

const live = { kind: "live", preset: "1h" } as const;

describe("system pass-through", () => {
  afterEach(() => vi.clearAllMocks());

  it("sends the system it is given, whatever the window's URL says", async () => {
    window.history.replaceState(null, "", "/servers?system=example-prod");
    await listServers("example-staging", live);
    await getLensInfo("example-staging");
    await muteFinding("example-staging", { checkCode: "stream-005", reason: "r", expiresAt: Date.now() + 3_600_000, mutedBy: "op" });
    await unmuteFinding("example-staging", "m1", "op");
    expect(vi.mocked(lens.listServers).mock.calls[0][0]).toMatchObject({ system: "example-staging" });
    expect(vi.mocked(lens.getLensInfo).mock.calls[0][0]).toMatchObject({ system: "example-staging" });
    expect(vi.mocked(lens.muteFinding).mock.calls[0][0]).toMatchObject({ system: "example-staging" });
    expect(vi.mocked(lens.unmuteFinding).mock.calls[0][0]).toMatchObject({ system: "example-staging" });
    window.history.replaceState(null, "", "/");
  });

  it("sends no system for the empty one", async () => {
    await listServers("", live);
    expect(vi.mocked(lens.listServers).mock.calls[0][0]).not.toHaveProperty("system");
  });
});

describe("getShellStatus", () => {
  afterEach(() => vi.clearAllMocks());
  const interval = durationFromMs(30_000);

  it("sends no range, so the server reads the last two collect intervals, and adapts the systems", async () => {
    vi.mocked(lens.getShellStatus).mockResolvedValue(
      create(GetShellStatusResponseSchema, {
        freshness: { epoch: 9n, epochAt: timestampFromMs(Date.now() - 5_000), live: true, interval, collecting: true },
        systems: [
          { system: { id: "example-prod", name: "Production" }, status: Status.WARNING, collection: CollectionState.COLLECTING, collecting: true },
          { system: { id: "example-staging" }, error: "the summary took longer than 5s" },
        ],
        defaultSystem: "example-prod",
        systemKnown: true,
      }),
    );
    const s = await getShellStatus("example-prod");
    expect(lens.getShellStatus).toHaveBeenCalledTimes(1);
    expect(vi.mocked(lens.getShellStatus).mock.calls[0][0]).toEqual({ system: "example-prod", range: undefined });
    expect(s.freshness).toMatchObject({ epoch: "9", intervalMs: 30_000 });
    expect(s.systems).toEqual([
      { id: "example-prod", name: "Production", environment: "", status: "warning", collection: "collecting", collecting: true, error: "" },
      { id: "example-staging", name: "example-staging", environment: "", status: "nodata", collection: "unknown", collecting: false, error: "the summary took longer than 5s" },
    ]);
    expect(s).toMatchObject({ defaultSystem: "example-prod", systemKnown: true });
  });
});

describe("getLastData", () => {
  afterEach(() => vi.clearAllMocks());
  const interval = durationFromMs(30_000);
  const reply = (since: number) =>
    create(GetShellStatusResponseSchema, {
      freshness: { epoch: 7n, epochAt: timestampFromMs(since - 20_000), live: true, interval, collecting: false, notCollectingSince: timestampFromMs(since) },
      servers: [create(ServerSummarySchema, { name: "n1", present: true }), create(ServerSummarySchema, { name: "n2", present: true })],
    });
  const startOf = () => Number(vi.mocked(lens.getShellStatus).mock.calls[0][0].range!.start!.seconds) * 1000;

  it("reads the newest collection before the stop, with its servers, from three intervals before it", async () => {
    const since = Date.now() - 3 * 3_600_000;
    vi.mocked(lens.getShellStatus).mockResolvedValue(reply(since));
    const s = await getLastData("example-staging", since, 30_000);
    expect(startOf()).toBe(Math.floor((since - 90_000) / 1000) * 1000);
    expect(s.servers.map((x) => x.name)).toEqual(["n1", "n2"]);
    expect(s.freshness).toMatchObject({ epoch: "7", epochAt: since - 20_000 });
  });

  it("looks back at most two days, whenever the collector stopped", async () => {
    const since = Date.now() - 20 * 86_400_000;
    vi.mocked(lens.getShellStatus).mockResolvedValue(reply(since));
    const before = Date.now();
    await getLastData("example-staging", since, 30_000);
    expect(startOf()).toBeGreaterThanOrEqual(Math.floor((before - 2 * 86_400_000) / 1000) * 1000);
  });
});

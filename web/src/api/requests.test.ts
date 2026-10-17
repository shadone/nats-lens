import { create } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import { afterEach, describe, expect, it, vi } from "vitest";
import { GetOverviewResponseSchema } from "../gen/natslens/v1/overview_pb";
import { ServerSummarySchema } from "../gen/natslens/v1/servers_pb";
import { lens } from "./client";
import { getHeaderStatus, getLensInfo, listServers, muteFinding, unmuteFinding } from "./requests";

vi.mock("./client", () => ({
  lens: {
    listServers: vi.fn(async () => ({ servers: [], expected: [], missing: [] })),
    getLensInfo: vi.fn(async () => ({})),
    muteFinding: vi.fn(async () => ({})),
    unmuteFinding: vi.fn(async () => ({})),
    getOverview: vi.fn(),
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

describe("getHeaderStatus", () => {
  afterEach(() => vi.clearAllMocks());
  const interval = durationFromMs(30_000);

  it("asks only the last two collect intervals while the collector collects", async () => {
    vi.mocked(lens.getOverview).mockResolvedValue(
      create(GetOverviewResponseSchema, { freshness: { epoch: 9n, epochAt: timestampFromMs(Date.now() - 5_000), live: true, interval, collecting: true } }),
    );
    const s = await getHeaderStatus("example-prod", 30_000);
    expect(lens.getOverview).toHaveBeenCalledTimes(1);
    expect(s.freshness?.epoch).toBe("9");
  });

  it("reads the newest collection before the stop, with its servers, while the collector does not collect", async () => {
    const since = Date.now() - 3 * 3_600_000;
    vi.mocked(lens.getOverview)
      .mockResolvedValueOnce(create(GetOverviewResponseSchema, { freshness: { live: true, interval, collecting: false, notCollectingSince: timestampFromMs(since) } }))
      .mockResolvedValueOnce(
        create(GetOverviewResponseSchema, {
          freshness: { epoch: 7n, epochAt: timestampFromMs(since - 20_000), live: true, interval, collecting: false, notCollectingSince: timestampFromMs(since) },
          servers: [create(ServerSummarySchema, { name: "n1", present: true }), create(ServerSummarySchema, { name: "n2", present: true })],
        }),
      );
    const s = await getHeaderStatus("example-staging", 30_000);
    expect(lens.getOverview).toHaveBeenCalledTimes(2);
    const second = vi.mocked(lens.getOverview).mock.calls[1][0];
    expect(Number(second.range!.start!.seconds) * 1000).toBe(Math.floor((since - 90_000) / 1000) * 1000);
    expect(s.servers.map((x) => x.name)).toEqual(["n1", "n2"]);
    expect(s.freshness).toMatchObject({ epoch: "7", collecting: false, notCollectingSince: since });
  });
});

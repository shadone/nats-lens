import { afterEach, describe, expect, it, vi } from "vitest";
import { lens } from "./client";
import { getLensInfo, listServers, muteFinding, unmuteFinding } from "./requests";

vi.mock("./client", () => ({
  lens: {
    listServers: vi.fn(async () => ({ servers: [], expected: [], missing: [] })),
    getLensInfo: vi.fn(async () => ({})),
    muteFinding: vi.fn(async () => ({})),
    unmuteFinding: vi.fn(async () => ({})),
  },
}));

const live = { kind: "live", preset: "1h" } as const;

describe("system pass-through", () => {
  afterEach(() => vi.clearAllMocks());

  it("sends the system it is given, whatever the window's URL says", async () => {
    window.history.replaceState(null, "", "/servers?system=example-prod");
    await listServers("example-staging", live);
    await getLensInfo("example-staging");
    await muteFinding("example-staging", { checkCode: "stream-025", reason: "r", expiresAt: Date.now() + 3_600_000, mutedBy: "op" });
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

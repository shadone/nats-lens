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
  afterEach(() => {
    window.history.replaceState(null, "", "/");
    vi.clearAllMocks();
  });

  it("sends the ?system= of the page URL", async () => {
    window.history.replaceState(null, "", "/servers?system=example-staging&range=1h");
    await listServers(live);
    await getLensInfo();
    await muteFinding({ checkCode: "stream-025", reason: "r", expiresAt: Date.now() + 3_600_000, mutedBy: "op" });
    await unmuteFinding("m1", "op");
    expect(vi.mocked(lens.listServers).mock.calls[0][0]).toMatchObject({ system: "example-staging" });
    expect(vi.mocked(lens.getLensInfo).mock.calls[0][0]).toMatchObject({ system: "example-staging" });
    expect(vi.mocked(lens.muteFinding).mock.calls[0][0]).toMatchObject({ system: "example-staging" });
    expect(vi.mocked(lens.unmuteFinding).mock.calls[0][0]).toMatchObject({ system: "example-staging" });
  });

  it("sends no system without the parameter", async () => {
    await listServers(live);
    expect(vi.mocked(lens.listServers).mock.calls[0][0]).not.toHaveProperty("system");
  });
});

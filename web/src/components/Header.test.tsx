import { Code, ConnectError } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Header } from "./Header";

vi.mock("../api/client", () => ({
  lens: { listAccounts: vi.fn(async () => ({ accounts: [] })) },
}));
vi.mock("../api/requests", async (orig) => ({
  ...(await orig<typeof import("../api/requests")>()),
  getLensInfo: vi.fn(),
  getShellStatus: vi.fn(),
  getOverview: vi.fn(),
}));
const m = vi.mocked(requests);
const tally = { critical: 0, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0 };
const prod = { id: "example-prod", name: "Production", environment: "", status: "ok", collection: "collecting", collecting: true, error: "", pending: false } as const;

describe("Header", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "dev", cluster: "example-prod", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });
  });

  it("reads the light status call of the page's system, not the page's overview", async () => {
    m.getShellStatus.mockResolvedValue({
      freshness: { epoch: "1", epochAt: Date.now() - 2000, partial: false, live: true, intervalMs: 30_000, collecting: true },
      servers: [],
      tally,
      systems: [prod],
      defaultSystem: "example-prod",
      systemKnown: true,
    });
    renderApp(<Header />, "/streams?range=6h&system=example-prod");
    await waitFor(() => expect(document.querySelector(".live")).toHaveTextContent(/^live · \ds$/));
    expect(m.getShellStatus).toHaveBeenCalledWith("example-prod", undefined, expect.anything());
    expect(m.getOverview).not.toHaveBeenCalled();
  });

  it("says offline when the status call fails", async () => {
    m.getShellStatus.mockRejectedValue(new ConnectError("refused", Code.Unavailable));
    renderApp(<Header />, "/");
    expect(await screen.findByText("offline")).toBeInTheDocument();
  });

  it("says no data for a system that is not configured, and not offline", async () => {
    m.getShellStatus.mockResolvedValue({ servers: [], tally, systems: [prod], defaultSystem: "example-prod", systemKnown: false });
    renderApp(<Header />, "/streams?system=nope");
    await waitFor(() => expect(m.getShellStatus).toHaveBeenCalledWith("nope", undefined, expect.anything()));
    await new Promise((r) => setTimeout(r, 20));
    expect(screen.queryByText("offline")).toBeNull();
    expect(document.querySelector(".live")).toHaveTextContent("no data");
  });

  it("names the page and holds the account scope, search, range and theme", () => {
    m.getShellStatus.mockReturnValue(new Promise(() => {}));
    renderApp(<Header />, "/consumers");
    const bar = screen.getByRole("banner");
    expect(bar).toHaveTextContent(/^Consumers/);
    expect(screen.getByRole("button", { name: "All accounts" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Search this system" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Theme: / })).toBeInTheDocument();
  });

  it("has no account scope on Fleet, which shows every system", () => {
    m.getShellStatus.mockReturnValue(new Promise(() => {}));
    renderApp(<Header />, "/fleet");
    expect(screen.getByRole("banner")).toHaveTextContent(/^Fleet/);
    expect(screen.queryByRole("button", { name: /accounts/ })).toBeNull();
  });
});

import { Code, ConnectError } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Header } from "./Header";

vi.mock("../api/client", () => ({
  lens: { listSystems: vi.fn(async () => ({ systems: [{ system: { id: "example-prod", name: "Production", environment: "" } }], defaultSystem: "example-prod" })), listAccounts: vi.fn(async () => ({ accounts: [] })) },
}));
vi.mock("../api/requests", async (orig) => ({
  ...(await orig<typeof import("../api/requests")>()),
  getLensInfo: vi.fn(),
  getHeaderStatus: vi.fn(),
  getOverview: vi.fn(),
}));
const m = vi.mocked(requests);
const tally = { critical: 0, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0 };

describe("Header", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "dev", cluster: "example-prod", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });
  });

  it("reads the light status call of the page's system, not the page's overview", async () => {
    m.getHeaderStatus.mockResolvedValue({
      cluster: "example-prod",
      freshness: { epoch: "1", epochAt: Date.now() - 2000, partial: false, live: true, intervalMs: 30_000 },
      servers: [],
      tally,
    });
    renderApp(<Header />, "/streams?range=6h&system=example-prod");
    await waitFor(() => expect(document.querySelector(".live")).toHaveTextContent(/^live · \ds$/));
    expect(m.getHeaderStatus).toHaveBeenCalledWith("example-prod", 30_000, expect.anything());
    expect(m.getOverview).not.toHaveBeenCalled();
  });

  it("says offline when the status call fails", async () => {
    m.getHeaderStatus.mockRejectedValue(new ConnectError("refused", Code.Unavailable));
    renderApp(<Header />, "/");
    expect(await screen.findByText("offline")).toBeInTheDocument();
  });

  it("names the page and holds the account scope, search, range and theme", () => {
    m.getHeaderStatus.mockReturnValue(new Promise(() => {}));
    renderApp(<Header />, "/consumers");
    const bar = screen.getByRole("banner");
    expect(bar).toHaveTextContent(/^Consumers/);
    expect(screen.getByRole("button", { name: "All accounts" })).toBeInTheDocument();
    expect(screen.getByRole("combobox", { name: "Search this system" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Theme: / })).toBeInTheDocument();
  });

  it("has no account scope on Fleet, which shows every system", () => {
    m.getHeaderStatus.mockReturnValue(new Promise(() => {}));
    renderApp(<Header />, "/fleet");
    expect(screen.getByRole("banner")).toHaveTextContent(/^Fleet/);
    expect(screen.queryByRole("button", { name: /accounts/ })).toBeNull();
  });
});

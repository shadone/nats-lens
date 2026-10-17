import { Code, ConnectError } from "@connectrpc/connect";
import { screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Header } from "./Header";

vi.mock("../api/requests", () => ({ getLensInfo: vi.fn(), getHeaderStatus: vi.fn(), getOverview: vi.fn() }));
const m = vi.mocked(requests);

describe("Header", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    m.getLensInfo.mockResolvedValue({ identityMode: "request", version: "dev", cluster: "example-prod", collectIntervalMs: 30_000, retentionMs: 0, live: true, demo: true });
  });

  it("reads lens info and the light status call, not the page's overview", async () => {
    m.getHeaderStatus.mockResolvedValue({
      cluster: "example-prod",
      freshness: { epoch: "1", epochAt: Date.now() - 2000, partial: false, live: true, intervalMs: 30_000 },
      servers: [],
    });
    renderApp(<Header />, "/streams?range=6h");
    await waitFor(() => expect(document.querySelector(".live")).toHaveTextContent(/^live · \ds$/));
    expect(screen.getByText("example-prod")).toBeInTheDocument();
    expect(screen.getByText("demo")).toBeInTheDocument();
    expect(m.getHeaderStatus).toHaveBeenCalledWith(30_000, expect.anything());
    expect(m.getOverview).not.toHaveBeenCalled();
  });

  it("says offline when the status call fails", async () => {
    m.getHeaderStatus.mockRejectedValue(new ConnectError("refused", Code.Unavailable));
    renderApp(<Header />, "/");
    expect(await screen.findByText("offline")).toBeInTheDocument();
  });

  it("labels the server chips as now under a fixed range", async () => {
    m.getHeaderStatus.mockResolvedValue({
      cluster: "c",
      freshness: { epoch: "1", epochAt: Date.now(), partial: false, live: true, intervalMs: 30_000 },
      servers: [{ name: "nats-0", serverId: "x", version: "2.12.2", cluster: "c", role: "follower", present: true, healthz: "ok", healthzError: "", restarts: [], status: "ok" }],
    });
    renderApp(<Header />, "/?from=2026-10-16T09:00:00Z&to=2026-10-16T10:00:00Z");
    await waitFor(() => expect(screen.getByLabelText("Servers now")).toHaveTextContent(/^now\s*nats-0/));
  });

  it("dims the server chips when the status call fails", async () => {
    m.getHeaderStatus.mockRejectedValue(new ConnectError("refused", Code.Unavailable));
    renderApp(<Header />, "/");
    await screen.findByText("offline");
    expect(screen.getByLabelText(/^Servers/)).toHaveClass("stale");
  });
});

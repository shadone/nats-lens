// The shell's status keeps the last data of a system whose collector
// stopped, and looks it up once per stop, not on every refresh.
import { act, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderApp } from "../test/render";
import { useShellStatus } from "./queries";
import * as requests from "./requests";

vi.mock("./requests", async (orig) => ({
  ...(await orig<typeof import("./requests")>()),
  getShellStatus: vi.fn(),
  getLastData: vi.fn(),
}));
const m = vi.mocked(requests);
const tally = { critical: 0, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0 };
const since = Date.now() - 3_600_000;

let refetch: () => unknown = () => {};
function Probe() {
  const s = useShellStatus();
  refetch = s.refetch;
  return (
    <div data-testid="status">
      {s.data?.servers.length ?? "-"} servers, newest {s.data?.freshness?.epoch ?? "-"}
    </div>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  m.getShellStatus.mockResolvedValue({
    freshness: { epoch: "0", partial: false, live: true, intervalMs: 30_000, collecting: false, notCollectingSince: since },
    servers: [],
    tally,
    systems: [],
    defaultSystem: "c",
    systemKnown: true,
  });
  m.getLastData.mockResolvedValue({
    freshness: { epoch: "7", epochAt: since - 20_000, partial: false, live: true, intervalMs: 30_000, collecting: false, notCollectingSince: since },
    servers: [{ name: "n1" }, { name: "n2" }] as never,
  });
});

describe("useShellStatus while the collector does not collect", () => {
  it("shows the servers and the time of the last data, looked up once however often it refreshes", async () => {
    renderApp(<Probe />, "/");
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("2 servers, newest 7"));
    for (let i = 0; i < 3; i++) await act(async () => void (await refetch()));
    expect(m.getShellStatus.mock.calls.length).toBeGreaterThanOrEqual(4);
    expect(m.getLastData).toHaveBeenCalledTimes(1);
    expect(m.getLastData).toHaveBeenCalledWith("", since, 30_000, expect.anything());
    expect(screen.getByTestId("status")).toHaveTextContent("2 servers, newest 7");
  });

  it("looks back by the collect interval the status names", async () => {
    m.getShellStatus.mockResolvedValue({
      freshness: { epoch: "0", partial: false, live: true, intervalMs: 60_000, collecting: false, notCollectingSince: since },
      servers: [],
      tally,
      systems: [],
      defaultSystem: "c",
      systemKnown: true,
    });
    renderApp(<Probe />, "/");
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("2 servers, newest 7"));
    expect(m.getLastData).toHaveBeenCalledWith("", since, 60_000, expect.anything());
  });

  it("asks nothing more while the collector collects", async () => {
    m.getShellStatus.mockResolvedValue({
      freshness: { epoch: "9", epochAt: Date.now(), partial: false, live: true, intervalMs: 30_000, collecting: true },
      servers: [],
      tally,
      systems: [],
      defaultSystem: "c",
      systemKnown: true,
    });
    renderApp(<Probe />, "/");
    await waitFor(() => expect(screen.getByTestId("status")).toHaveTextContent("newest 9"));
    expect(m.getLastData).not.toHaveBeenCalled();
  });
});

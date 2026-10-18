// The server and stream filters: the chosen value stays on offer while the
// choices load, when their call fails and when the range lacks it, and a
// system switch shows the new system's choices.
import { act, screen, waitFor, within } from "@testing-library/react";
import { useNavigate } from "react-router";
import { describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { renderApp } from "../test/render";
import { Connections } from "./Connections";
import { Consumers } from "./Consumers";

vi.mock("../api/requests", () => ({
  listConnections: vi.fn(),
  listServers: vi.fn(),
  listConsumers: vi.fn(),
  listStreams: vi.fn(),
  getLensInfo: vi.fn(() => new Promise(() => {})),
}));

const select = (name: string) => screen.getByRole("combobox", { name }) as HTMLSelectElement;
const opts = (name: string) => within(select(name)).getAllByRole("option").map((o) => o.textContent);
let go: ReturnType<typeof useNavigate> = () => {};
function Nav() {
  go = useNavigate();
  return null;
}
const page = (servers: string[]) => ({ items: [], nextPageToken: "", totalCount: 0, epoch: "1", toolingHidden: 0, servers });
const ent = (stream: string) => ({ kind: "stream", key: `APP/${stream}`, name: stream, account: "APP", accountName: "APP", stream, consumer: "", server: "" });

describe("Connections server filter choices", () => {
  it("offers the chosen server while the connections load", () => {
    vi.mocked(requests.listConnections).mockImplementation(() => new Promise(() => {}));
    renderApp(<Connections />, "/connections?server=nats-1");
    expect(opts("Server")).toEqual(["All servers", "nats-1"]);
    expect(select("Server")).toHaveValue("nats-1");
  });

  it("offers the chosen server when the call fails", async () => {
    vi.mocked(requests.listConnections).mockRejectedValue(new Error("boom"));
    renderApp(<Connections />, "/connections?server=nats-1");
    await screen.findByText(/boom/);
    expect(opts("Server")).toEqual(["All servers", "nats-1"]);
    expect(select("Server")).toHaveValue("nats-1");
  });

  it("offers a chosen server the range does not name after the others", async () => {
    vi.mocked(requests.listConnections).mockResolvedValue(page(["nats-0"]));
    renderApp(<Connections />, "/connections?server=nats-9");
    await waitFor(() => expect(opts("Server")).toEqual(["All servers", "nats-0", "nats-9"]));
    expect(select("Server")).toHaveValue("nats-9");
  });

  it("shows the new system's servers once it answers", async () => {
    let releaseB: (v: unknown) => void = () => {};
    vi.mocked(requests.listConnections).mockImplementation(async (sys) =>
      sys === "sys-a" ? page(["a-0", "a-1"]) : (new Promise((r) => (releaseB = r as never)) as never),
    );
    renderApp(
      <>
        <Connections />
        <Nav />
      </>,
      "/connections?system=sys-a",
    );
    await waitFor(() => expect(opts("Server")).toEqual(["All servers", "a-0", "a-1"]));
    act(() => go("/connections?system=sys-b"));
    await waitFor(() => expect(requests.listConnections).toHaveBeenCalledWith("sys-b", expect.anything(), expect.anything(), expect.anything()));
    await act(async () => releaseB(page(["b-0"])));
    await waitFor(() => expect(opts("Server")).toEqual(["All servers", "b-0"]));
  });
});

describe("Consumers stream filter choices", () => {
  it("offers the chosen stream while the streams load", () => {
    vi.mocked(requests.listConsumers).mockResolvedValue([]);
    vi.mocked(requests.listStreams).mockImplementation(() => new Promise(() => {}));
    renderApp(<Consumers />, "/consumers?stream=APP/order-jobs");
    expect(opts("Stream")).toEqual(["All streams", "order-jobs (APP)"]);
    expect(select("Stream")).toHaveValue("APP/order-jobs");
  });

  it("offers the chosen stream when the call fails, and still asks for the consumers", async () => {
    vi.mocked(requests.listConsumers).mockClear().mockResolvedValue([]);
    vi.mocked(requests.listStreams).mockRejectedValue(new Error("streams down"));
    renderApp(<Consumers />, "/consumers?stream=APP/order-jobs");
    await waitFor(() => expect(requests.listStreams).toHaveBeenCalled());
    await waitFor(() => expect(requests.listConsumers).toHaveBeenCalled());
    expect(opts("Stream")).toEqual(["All streams", "order-jobs (APP)"]);
    expect(select("Stream")).toHaveValue("APP/order-jobs");
  });

  it("offers a chosen stream the range does not hold after the others", async () => {
    vi.mocked(requests.listConsumers).mockResolvedValue([]);
    vi.mocked(requests.listStreams).mockResolvedValue({ streams: [{ entity: ent("order-archive") }], totals: {} } as never);
    renderApp(<Consumers />, "/consumers?stream=APP/order-jobs");
    await waitFor(() => expect(opts("Stream")).toEqual(["All streams", "order-archive (APP)", "order-jobs (APP)"]));
    expect(select("Stream")).toHaveValue("APP/order-jobs");
  });

  it("shows the new system's streams and reads the old system's from the cache", async () => {
    vi.mocked(requests.listConsumers).mockResolvedValue([]);
    vi.mocked(requests.listStreams).mockClear();
    vi.mocked(requests.listStreams).mockImplementation(async (sys) => ({ streams: [{ entity: ent(sys === "sys-a" ? "a-stream" : "b-stream") }], totals: {} }) as never);
    renderApp(
      <>
        <Consumers />
        <Nav />
      </>,
      "/consumers?system=sys-a",
    );
    await waitFor(() => expect(opts("Stream")).toEqual(["All streams", "a-stream (APP)"]));
    act(() => go("/consumers?system=sys-b"));
    await waitFor(() => expect(opts("Stream")).toEqual(["All streams", "b-stream (APP)"]));
    act(() => go("/consumers?system=sys-a"));
    await waitFor(() => expect(opts("Stream")).toEqual(["All streams", "a-stream (APP)"]));
    expect(vi.mocked(requests.listStreams).mock.calls.map((c) => c[0])).toEqual(["sys-a", "sys-b"]);
  });
});

// Topology across refetches and with unusual data: a cluster without a
// name, a gateway pair fully down, a map that appears on a later refetch.
// The first cases came from the review of the page.
import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useQueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { cluster, gateway, global, route, single } from "../test/fixtures/topology";
import { renderApp } from "../test/render";
import { Topology } from "./Topology";

vi.mock("../api/requests", () => ({ getTopology: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

function Refetch() {
  const qc = useQueryClient();
  return (
    <button type="button" onClick={() => qc.invalidateQueries()}>
      refetch
    </button>
  );
}

function renderWithRefetch() {
  return renderApp(
    <>
      <Topology />
      <Refetch />
    </>,
    "/topology",
  );
}

async function refetch() {
  await act(async () => userEvent.click(screen.getByRole("button", { name: "refetch" })));
}

afterEach(() => vi.mocked(requests.getTopology).mockReset());

describe("Topology at a phone width", () => {
  let orig: typeof Element.prototype.getBoundingClientRect;
  beforeEach(() => {
    orig = Element.prototype.getBoundingClientRect;
    Element.prototype.getBoundingClientRect = () => ({ width: 390, height: 100, x: 0, y: 0, top: 0, left: 0, right: 390, bottom: 100, toJSON: () => ({}) }) as DOMRect;
    vi.stubGlobal(
      "ResizeObserver",
      class {
        observe() {}
        disconnect() {}
        unobserve() {}
      },
    );
  });
  afterEach(() => {
    Element.prototype.getBoundingClientRect = orig;
    vi.unstubAllGlobals();
  });

  it("draws the grid when the map is there from the start", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(global());
    renderApp(<Topology />, "/topology");
    await screen.findByText(/3 clusters joined by gateways/);
    expect(document.querySelector(".tm-grid")).not.toBeNull();
  });

  it("draws the grid for a map that first appears on a refetch", async () => {
    vi.mocked(requests.getTopology).mockResolvedValueOnce(single()).mockResolvedValue(global());
    renderWithRefetch();
    await screen.findByText(/One cluster/);
    await refetch();
    await screen.findByText(/3 clusters joined by gateways/);
    expect(document.querySelector(".tm-grid")).not.toBeNull();
  });
});

describe("Topology's routes cluster", () => {
  it("stays on one cluster when a live refetch reorders equal routes", async () => {
    const first = global();
    first.routes = [route("eu", "nats-eu-0", "nats-eu-1", { rttMs: 0.61 }), route("us", "nats-us-0", "nats-us-1", { rttMs: 0.6 })];
    const second = global();
    second.routes = [route("us", "nats-us-0", "nats-us-1", { rttMs: 0.62 }), route("eu", "nats-eu-0", "nats-eu-1", { rttMs: 0.6 })];
    vi.mocked(requests.getTopology).mockResolvedValueOnce(first).mockResolvedValue(second);
    renderWithRefetch();
    expect(await screen.findByRole("heading", { name: "Routes inside eu" })).toBeInTheDocument();
    await refetch();
    await new Promise((r) => setTimeout(r, 50));
    expect(vi.mocked(requests.getTopology)).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("heading", { level: 2, name: /Routes inside/ })).toHaveTextContent("Routes inside eu");
  });

  it("opens on the cluster of a slow route, else the meta leader's", async () => {
    const t = global();
    t.routes = [route("eu", "nats-eu-0", "nats-eu-1", { rttMs: 0.9 }), route("us", "nats-us-0", "nats-us-1", { rttMs: 4.1, slow: true })];
    vi.mocked(requests.getTopology).mockResolvedValue(t);
    renderApp(<Topology />, "/topology");
    expect(await screen.findByRole("heading", { name: "Routes inside us" })).toBeInTheDocument();
  });
});

describe("Topology with a cluster without a name and a gateway pair down", () => {
  function data() {
    const t = global();
    t.clusters = [cluster("", ["nats-x-0", "nats-x-1"]), ...t.clusters];
    t.routes = [route("", "nats-x-0", "nats-x-1"), ...t.routes.map((r) => ({ ...r, slow: false }))];
    t.gateways = [gateway("", "eu", 0, { connections: 0, expected: 4, rttMedianMs: undefined, rttMaxMs: undefined }), ...t.gateways];
    return t;
  }

  it("names the cluster without a name the same way everywhere", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(data());
    const user = userEvent.setup();
    renderApp(<Topology />, "/topology");
    const box = await screen.findByRole("button", { name: "unknown cluster, show its routes" });
    expect(screen.getByRole("option", { name: "unknown cluster" })).toBeInTheDocument();
    expect(screen.getByRole("list", { name: "Servers of unknown cluster" })).toBeInTheDocument();
    expect(screen.getByText("gateway to unknown cluster down", { exact: false })).toBeInTheDocument();
    await user.click(box);
    expect(await screen.findByRole("heading", { name: "Routes inside unknown cluster" })).toBeInTheDocument();
    expect(screen.getByTestId("location")).toHaveTextContent("/topology?cluster=");
  });

  it("draws a pair with no connection up as down, in the warning colour on both clusters", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(data());
    renderApp(<Topology />, "/topology");
    await screen.findByText(/4 clusters/);
    const label = [...document.querySelectorAll(".tm-gw-label > [aria-hidden]")].find((e) => e.textContent === "gateway down · 0 of 4 up");
    expect(label).toBeDefined();
    expect(label!.closest(".tm-gw-label")).toHaveClass("warn");
    expect(label!.closest(".tm-gw-label")).toHaveAccessibleName(/Gateway unknown cluster – eu: 0 of 4 connections up; down/);
    const unknown = screen.getByRole("button", { name: "unknown cluster, show its routes" }).closest(".tm-box");
    const eu = screen.getByRole("button", { name: "eu, show its routes" }).closest(".tm-box");
    expect(unknown).toHaveClass("warn");
    expect(eu).toHaveClass("warn");
    expect(within(eu as HTMLElement).getByText(/gateway to unknown cluster down/)).toBeInTheDocument();
    expect(within(unknown as HTMLElement).getByText(/gateway to eu down/)).toBeInTheDocument();
    expect(screen.getByText(/4 clusters joined by gateways, 1 gateway pair down, 4 leaf node sites/)).toBeInTheDocument();
  });

  it("does not say the clusters are joined when no gateway connection is up", async () => {
    const t = global();
    t.gateways = t.gateways.map((g) => ({ ...g, connections: 0, rttMedianMs: undefined, rttMaxMs: undefined }));
    vi.mocked(requests.getTopology).mockResolvedValue(t);
    renderApp(<Topology />, "/topology");
    expect(await screen.findByText(/^3 clusters, every gateway pair down, 4 leaf node sites at the edge/)).toBeInTheDocument();
  });
});

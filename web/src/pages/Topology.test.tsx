import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ConnectError, Code } from "@connectrpc/connect";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { TopologyMap } from "../components/TopologyMap";
import { cluster, gateway, global, empty, single } from "../test/fixtures/topology";
import { renderApp } from "../test/render";
import { Topology } from "./Topology";

vi.mock("../api/requests", () => ({ getTopology: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

afterEach(() => vi.mocked(requests.getTopology).mockReset());

/** The map label whose visible text is text, as its parts read together. */
function gwLabel(text: string): HTMLElement {
  const el = [...document.querySelectorAll<HTMLElement>(".tm-gw-label")].find((e) => e.querySelector("[aria-hidden]")?.textContent === text);
  if (!el) throw new Error(`no gateway label "${text}"`);
  return el;
}

function table(name: string) {
  return within(screen.getByRole("region", { name }));
}

function bodyRows(name: string) {
  return table(name).getAllByRole("row").slice(1);
}

describe("Topology", () => {
  it("says what the system is made of and draws a box per cluster with labelled gateways", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(global());
    renderApp(<Topology />, "/topology?system=example-global");
    expect(await screen.findByText(/3 clusters joined by gateways, 4 leaf node sites at the edge, in the collection of/)).toBeInTheDocument();
    const map = within(screen.getByRole("region", { name: /Map of clusters/ }));
    for (const c of ["ap", "eu", "us"]) expect(map.getByRole("button", { name: `${c}, show its routes` })).toBeInTheDocument();
    expect(map.getByText("gateway 84 ms")).toBeInTheDocument();
    expect(map.getByText("gateway 166 ms")).toBeInTheDocument();
    expect(map.getByText("JetStream meta leader · 5 streams led")).toBeInTheDocument();
    expect(map.getByText("Leaf nodes on eu").parentElement).toHaveTextContent("3 sites · 1 not connected");
    expect(map.getByText("Leaf nodes on us").parentElement).toHaveTextContent("1 site · all connected");
    // ap has no sites: no leaf box.
    expect(map.queryByText("Leaf nodes on ap")).toBeNull();
    // A server opens its page, keeping the system.
    expect(map.getByRole("link", { name: "nats-eu-1" })).toHaveAttribute("href", "/servers/nats-eu-1?system=example-global");
    // How drops are seen, with the collect interval.
    expect(screen.getByText(/Drops are counted from collections every 30s; a drop and reconnect within one interval is seen only as a new connection ID\./)).toBeInTheDocument();
  });

  it("draws a gateway that dropped in the last hour in the warning colour, with its servers", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(global());
    renderApp(<Topology />, "/topology");
    await screen.findByText("gateway 84 ms");
    const label = gwLabel("gateway 212 ms · 3 drops/h");
    expect(label).toHaveClass("warn");
    expect(label.querySelectorAll(".nw")).toHaveLength(2);
    expect(label).toHaveAccessibleName(/Gateway ap – us: 6 of 6 connections up; .*3 drops in the last hour; 7 drops in 24 hours; dropping on nats-ap-1/);
    expect(screen.getByRole("link", { name: "nats-ap-1, gateway dropping" })).toHaveClass("warn");
    expect(screen.getByRole("link", { name: "nats-us-2, missing" })).toHaveClass("warn");
    expect(screen.getByRole("link", { name: "nats-ap-0" })).not.toHaveClass("warn");
    expect(screen.getByText("4 streams led · gateway to us dropping")).toBeInTheDocument();
    expect(screen.getByText("gateway 84 ms").parentElement).not.toHaveClass("warn");
  });

  it("lists a site that is not connected first, with a dash for its round trip and its last hub", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(global());
    renderApp(<Topology />, "/topology");
    await screen.findByText("Leaf nodes, worst first");
    const rows = bodyRows("Leaf nodes, worst first");
    expect(rows[0]).toHaveClass("warn");
    const cells = within(rows[0]).getAllByRole("cell");
    expect(within(rows[0]).getByRole("rowheader")).toHaveTextContent("pickup-0417not connected, last seen 09:12 on nats-eu-1");
    expect(within(cells[0]).getByRole("link", { name: "nats-eu-1" })).toBeInTheDocument();
    expect(cells[1]).toHaveTextContent(/^– not connected$/);
    expect(cells[2]).toHaveTextContent("17");
    expect(cells[3]).toHaveTextContent(/^– not connected$/);
    expect(rows[1]).not.toHaveClass("warn");
  });

  it("shows a value the server did not report as a dash, never as 0", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(global());
    renderApp(<Topology />, "/topology");
    await screen.findByText("Leaf nodes, worst first");
    const site = bodyRows("Leaf nodes, worst first")[2];
    expect(within(site).getByRole("rowheader")).toHaveTextContent("pickup-1208");
    const cells = within(site).getAllByRole("cell");
    expect(cells[2]).toHaveTextContent(/^– not observed$/);
    expect(cells[3]).toHaveTextContent(/^– not observed$/);
    const route = bodyRows("Routes inside eu")[2];
    expect(within(route).getByRole("rowheader")).toHaveTextContent("nats-eu-1 → nats-eu-2only nats-eu-1 reports it; nats-eu-2 does not");
    const rc = within(route).getAllByRole("cell");
    expect(rc[0]).toHaveTextContent(/^– not observed$/);
    expect(rc[1]).toHaveTextContent(/^– not observed$/);
    expect(rc[2]).toHaveTextContent("none");
  });

  it("marks a slow route and lists it first", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(global());
    renderApp(<Topology />, "/topology");
    await screen.findByText("Routes inside eu");
    const rows = bodyRows("Routes inside eu");
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveClass("warn");
    expect(within(rows[0]).getByRole("rowheader")).toHaveTextContent("nats-eu-0 → nats-eu-3");
    expect(within(rows[0]).getAllByRole("cell")[0]).toHaveTextContent("4.1 msslow");
    expect(rows[1]).not.toHaveClass("warn");
    expect(within(rows[1]).getAllByRole("cell")[3]).toHaveTextContent("APP");
  });

  it("picks the routes table's cluster with the select or a click on a cluster, and keeps it in the link", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(global());
    const user = userEvent.setup();
    renderApp(<Topology />, "/topology");
    await screen.findByText("Routes inside eu");
    expect(screen.getByRole("button", { name: "eu, show its routes" })).toHaveAttribute("aria-pressed", "true");
    await user.selectOptions(screen.getByRole("combobox", { name: "Cluster" }), "us");
    expect(await screen.findByText("Routes inside us")).toBeInTheDocument();
    expect(bodyRows("Routes inside us")).toHaveLength(1);
    expect(screen.getByTestId("location")).toHaveTextContent("/topology?cluster=us");
    // A click on the box, away from a server's link.
    await user.click(screen.getByText("4 streams led · gateway to us dropping"));
    expect(await screen.findByText("Routes inside ap")).toBeInTheDocument();
    expect(screen.getByText("No routes reported inside ap.")).toBeInTheDocument();
    // The keyboard reaches the cluster's name.
    screen.getByRole("button", { name: "eu, show its routes" }).focus();
    await user.keyboard("{Enter}");
    expect(await screen.findByText("Routes inside eu")).toBeInTheDocument();
  });

  it("shows only the routes for a single cluster without gateways or leaf nodes", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(single());
    renderApp(<Topology />, "/topology?system=example-prod");
    expect(await screen.findByText(/One cluster, example-prod, of 3 servers, in the collection of/)).toBeInTheDocument();
    expect(screen.getByText("Routes inside example-prod")).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: /Map of clusters/ })).toBeNull();
    expect(screen.queryByText("Leaf nodes, worst first")).toBeNull();
    expect(screen.queryByRole("combobox", { name: "Cluster" })).toBeNull();
    expect(screen.queryByText(/Drops are counted/)).toBeNull();
    expect(screen.getByText(/For a single cluster this page shows just the routes/)).toBeInTheDocument();
    expect(bodyRows("Routes inside example-prod")).toHaveLength(3);
  });

  it("hides the gateway lines when there are none, and the leaf boxes and table without sites", async () => {
    const t = global();
    vi.mocked(requests.getTopology).mockResolvedValue({
      ...t,
      gateways: [],
      leafnodes: [],
      leafnodesTotal: 0,
      clusters: t.clusters.map((c) => ({ ...c, leafConnected: 0, leafNotConnected: 0 })),
    });
    renderApp(<Topology />, "/topology");
    expect(await screen.findByText(/^3 clusters, in the collection of/)).toBeInTheDocument();
    expect(screen.queryByText(/^gateway/)).toBeNull();
    expect(screen.queryByText(/Leaf nodes on/)).toBeNull();
    expect(screen.queryByText("Leaf nodes, worst first")).toBeNull();
    expect(screen.getByText("Routes inside eu")).toBeInTheDocument();
  });

  it("says when the leaf node list was cut and names servers that did not answer", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue({ ...global(), leafnodesTotal: 1240, unobserved: [{ endpoint: "ROUTEZ", servers: ["nats-eu-4"] }] });
    renderApp(<Topology />, "/topology");
    expect(await screen.findByText("The 4 worst of 1,240 sites are listed.")).toBeInTheDocument();
    expect(screen.getByText("ROUTEZ did not answer on nats-eu-4.")).toBeInTheDocument();
  });

  it("says when the range holds no collection", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(empty());
    renderApp(<Topology />, "/topology");
    expect(await screen.findByText("No collection in this range.")).toBeInTheDocument();
    expect(screen.getByText(/How the servers connect, in the newest collection of the range\./)).toBeInTheDocument();
  });

  it("shows the error with a retry", async () => {
    vi.mocked(requests.getTopology).mockRejectedValueOnce(new ConnectError("the database is busy", Code.Unavailable)).mockResolvedValue(single());
    const user = userEvent.setup();
    renderApp(<Topology />, "/topology");
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/Could not load the topology\..*unavailable.*the database is busy/);
    await user.click(within(alert).getByRole("button", { name: "Retry" }));
    expect(await screen.findByText("Routes inside example-prod")).toBeInTheDocument();
  });

  it("shows the loading state", () => {
    vi.mocked(requests.getTopology).mockReturnValue(new Promise(() => {}));
    renderApp(<Topology />, "/topology");
    expect(screen.getByRole("status")).toHaveTextContent("Loading the topology…");
  });
});

describe("TopologyMap", () => {
  it("at a narrow width draws a grid of boxes and a table of the gateways", () => {
    renderApp(<TopologyMap t={global()} width={390} picked="eu" onPick={() => {}} />);
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows.map((r) => within(r).getByRole("rowheader").textContent)).toEqual(["ap – us", "ap – eu", "eu – us"]);
    expect(rows[0]).toHaveClass("warn");
    expect(within(rows[0]).getAllByRole("cell").map((c) => c.textContent)).toEqual(["212 ms", "6 of 6", "3", "7", "0 B"]);
    expect(screen.getByText("Leaf nodes on eu")).toBeInTheDocument();
    expect(screen.queryByText("gateway 84 ms")).toBeNull();
  });

  it("with more than four clusters draws the grid at any width", () => {
    const names = ["a", "b", "c", "d", "e"];
    const t = { ...global(), clusters: names.map((n) => cluster(n, [`nats-${n}-0`])), gateways: [gateway("a", "e", 90)] };
    renderApp(<TopologyMap t={t} width={1400} onPick={() => {}} />);
    expect(screen.getByRole("rowheader", { name: "a – e" })).toBeInTheDocument();
  });

  it("draws a gateway between clusters that are not neighbours under the boxes", () => {
    const t = { ...global(), gateways: [gateway("ap", "us", 212, { drops1h: 1, droppingServers: ["nats-ap-1"] })] };
    const { container } = renderApp(<TopologyMap t={t} width={1200} onPick={() => {}} />);
    expect(gwLabel("gateway 212 ms · 1 drop/h")).toBeInTheDocument();
    expect(container.querySelector("polyline.tm-gw-line.warn")).not.toBeNull();
    // The label sits in the middle of the gap right of ap, clear of the leaf node connector under eu.
    expect(gwLabel("gateway 212 ms · 1 drop/h").style.left).toMatch(/^30.95/);
    // Each part of a leaf box stays on one line.
    expect([...container.querySelectorAll(".tm-leaf .nw")].map((e) => e.textContent)).toEqual(["sites", "1 not connected", "site", "all connected"]);
  });
});

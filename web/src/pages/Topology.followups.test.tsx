// Topology: route drops, memory per server and replication between clusters.
import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import type { Topology as TopologyData } from "../api/topology";
import { lastHeard, replicationProblem, routeLineParts, serverMemory } from "../lib/topologyView";
import { cluster, copy, global, route } from "../test/fixtures/topology";
import { renderApp } from "../test/render";
import { Topology } from "./Topology";

vi.mock("../api/requests", () => ({ getTopology: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

afterEach(() => vi.mocked(requests.getTopology).mockReset());

const MIB = 1 << 20;

/** global() with eu's nats-eu-0 – nats-eu-1 route dropping, memory on every eu server, and copies into ap. */
function followups(): TopologyData {
  const t = global();
  t.routes = t.routes.map((r) => (r.a === "nats-eu-0" && r.b === "nats-eu-1" ? { ...r, drops1h: 2, drops24h: 3 } : { ...r, drops1h: 0, drops24h: 0 }));
  t.routes.push(route("us", "nats-us-0", "nats-us-2", { rttMs: undefined, pendingBytes: undefined, pool: undefined, down: true, drops1h: 0, drops24h: 1, reportedBy: [] }));
  const eu = t.clusters[1];
  eu.servers = eu.servers.map((s, i) => ({ ...s, memBytes: (100 + 10 * i) * MIB, jsMemoryBytes: 0, jsMaxMemoryBytes: 2048 * MIB }));
  eu.servers[3] = { ...eu.servers[3], memBytes: 900 * MIB, memHigh: true };
  t.replication = [
    copy("search-index", "ap", "catalog-changes", "us", { kind: "source", lag: 18_400, lagBefore: 12_100, fallingBehind: true }),
    copy("order-archive", "ap", "order-events", "eu"),
  ];
  return t;
}

async function region(name: string | RegExp) {
  return within(await screen.findByRole("region", { name }));
}

describe("Topology follow-ups", () => {
  it("shows route drops in the cluster box, its note and the routes table", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(followups());
    renderApp(<Topology />, "/topology?cluster=eu");
    const map = await region(/Map of clusters/);
    const euBox = (await map.findByRole("button", { name: "eu, show its routes" })).closest(".tm-box")!;
    expect(euBox).toHaveClass("warn");
    expect(euBox.querySelector(".tm-routes")).toHaveTextContent("routes 2.3 ms · 2 drops/h");
    expect(euBox.querySelector(".tm-routes .tp-warn")).toHaveTextContent("2 drops/h");
    expect(euBox.querySelector(".tm-note")).toHaveTextContent("JetStream meta leader · 5 streams led · route nats-eu-0 – nats-eu-1 dropping");
    // us: a pair down.
    const usBox = map.getByRole("button", { name: "us, show its routes" }).closest(".tm-box")!;
    expect(usBox.querySelector(".tm-routes")).toHaveTextContent("routes 0.3 ms · 1 down");
    expect(usBox.querySelector(".tm-note")).toHaveTextContent("route nats-us-0 – nats-us-2 down");

    const rows = (await region("Routes inside eu")).getAllByRole("row").slice(1);
    const dropping = rows.find((r) => r.textContent?.startsWith("nats-eu-0 → nats-eu-1"))!;
    expect(dropping).toHaveClass("warn");
    const cells = within(dropping).getAllByRole("cell");
    expect(cells[2]).toHaveTextContent(/^2$/);
    expect(cells[2]).toHaveClass("tp-warn");
    expect(cells[3]).toHaveTextContent(/^3$/);
  });

  it("lists a pair that is down first, with dashes for what it does not have", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(followups());
    renderApp(<Topology />, "/topology?cluster=us");
    const rows = (await screen.findByRole("region", { name: "Routes inside us" })).querySelectorAll("tbody tr");
    const down = [...rows].find((r) => r.textContent?.includes("nats-us-2"))!;
    expect(down).toHaveClass("warn");
    expect(within(down as HTMLElement).getByRole("rowheader")).toHaveTextContent("down: neither server lists a route to the other");
    const cells = within(down as HTMLElement).getAllByRole("cell");
    expect(cells[0]).toHaveTextContent(/^– not observed$/);
    expect(cells[3]).toHaveTextContent(/^1$/);
    expect(cells[4]).toHaveTextContent(/^– down$/);
  });

  it("shows each server's memory under its name and marks one far above the others", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(followups());
    renderApp(<Topology />, "/topology");
    const map = await region(/Map of clusters/);
    const plain = await map.findByRole("link", { name: "nats-eu-0, memory 100 MiB" });
    expect(plain).not.toHaveClass("warn");
    expect(plain).toHaveTextContent("nats-eu-0100 MiB");
    const high = map.getByRole("link", { name: "nats-eu-3, memory 900 MiB, memory far above the other servers" });
    expect(high).toHaveClass("warn");
    expect(high).toHaveAttribute("title", "nats-eu-3: memory far above the other servers: memory 900 MiB; JetStream memory 0 B of 2 GiB");
    // A server without a reading has no figure.
    expect(map.getByRole("link", { name: "nats-ap-0" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Capacity" })).toHaveAttribute("href", "/capacity");
  });

  it("lists mirrors and sources between clusters, those with a warning first, and notes them on the cluster", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(followups());
    renderApp(<Topology />, "/topology");
    const table = await screen.findByRole("region", { name: "Replication between clusters" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveClass("warn");
    expect(within(rows[0]).getByRole("rowheader")).toHaveTextContent(
      "catalog-changes on us → search-index on apfalling behind: 18,400 messages, 12,100 in the collection before",
    );
    expect(within(rows[0]).getAllByRole("cell").map((c) => c.textContent)).toEqual(["Source", "18,400", "800 ms ago", "APP"]);
    expect(rows[1]).not.toHaveClass("warn");
    expect(within(rows[1]).getByRole("link", { name: "order-events" })).toHaveAttribute("href", "/streams/APP/order-events");
    const ap = (await region(/Map of clusters/)).getByRole("button", { name: "ap, show its routes" }).closest(".tm-box")!;
    expect(ap.querySelector(".tm-note")).toHaveTextContent("4 streams led · gateway to us dropping · mirrors from eu · sources from us");
  });

  it("has no replication table without copies between clusters", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(global());
    renderApp(<Topology />, "/topology");
    await screen.findByRole("region", { name: /Map of clusters/ });
    expect(screen.queryByRole("region", { name: "Replication between clusters" })).toBeNull();
  });
});

describe("topology view rules", () => {
  it("words a copy's problems and when it last heard", () => {
    expect(replicationProblem(copy("a", "ap", "b", "eu"))).toBeUndefined();
    expect(replicationProblem(copy("a", "ap", "b", "eu", { notActive: true, lastActiveMs: 190_000 }))).toBe("not heard from its origin for 3m");
    expect(replicationProblem(copy("a", "ap", "b", "eu", { notActive: true, neverHeard: true, lastActiveMs: undefined }))).toBe("never heard from its origin");
    expect(replicationProblem(copy("a", "ap", "b", "eu", { error: "stream not found" }))).toBe("error: stream not found");
    expect(lastHeard(copy("a", "ap", "b", "eu", { lastActiveMs: 1200 }))).toBe("1.2 s ago");
    expect(lastHeard(copy("a", "ap", "b", "eu", { lastActiveMs: 190_000 }))).toBe("3m ago");
    expect(lastHeard(copy("a", "ap", "b", "eu", { neverHeard: true, lastActiveMs: undefined }))).toBe("never");
    expect(lastHeard(copy("a", "ap", "b", "eu", { lastActiveMs: undefined }))).toBeUndefined();
  });

  it("marks a JetStream memory store at 75% of its limit, and nothing without a reading", () => {
    const s = { name: "n", missing: false, metaLeader: false, memHigh: false, memBytes: 100 * MIB, jsMemoryBytes: 1536 * MIB, jsMaxMemoryBytes: 2048 * MIB };
    expect(serverMemory(s).warn).toBe("JetStream memory 75% of its limit");
    expect(serverMemory({ ...s, jsMemoryBytes: 1535 * MIB }).warn).toBeUndefined();
    expect(serverMemory({ ...s, memBytes: undefined })).toEqual({});
  });

  it("sums a cluster's route drops in the last hour and leaves out what was not observed", () => {
    const c = cluster("eu", ["a", "b", "c"]);
    const rs = [route("eu", "a", "b", { drops1h: 1 }), route("eu", "a", "c", { drops1h: 2, rttMs: 0.6 }), route("eu", "b", "c", { drops1h: undefined }), route("us", "x", "y", { drops1h: 5 })];
    expect(routeLineParts(c, rs)).toEqual({ parts: ["routes 0.4 ms", "3 drops/h"], warn: true });
    expect(routeLineParts(c, rs.map((r) => ({ ...r, drops1h: 0 })))).toEqual({ parts: ["routes 0.4 ms"], warn: false });
    expect(routeLineParts(cluster("ap", ["z"]), rs)).toEqual({ parts: [], warn: false });
  });
});

// Topology: route drops, memory per server and replication between clusters.
import { screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import type { Topology as TopologyData } from "../api/topology";
import { lastHeard, routeLineParts, serverMemory } from "../lib/topologyView";
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
  eu.servers[2] = { ...eu.servers[2], jsMemoryBytes: 80 * MIB };
  eu.servers[3] = { ...eu.servers[3], memBytes: 900 * MIB, memoryFindingId: "server-008:nats-eu-3", memoryFindingCode: "server-008" };
  eu.servers[1] = { ...eu.servers[1], memoryFindingId: "server-007:nats-eu-1", memoryFindingCode: "server-007", memoryFindingQuiet: true };
  t.replication = [
    copy("search-index", "ap", "catalog-changes", "us", {
      kind: "source",
      sourceId: "a1",
      lag: 18_400,
      lagBefore: 12_100,
      findingId: "stream-022:APP/search-index",
      findingCode: "stream-022",
      findingDetail: "Leader nats-ap-0: catalog-changes: not heard from its origin for 3m.",
    }),
    copy("search-index", "ap", "catalog-changes", "us", { kind: "source", sourceId: "b2", filterSubject: "catalog.eu.>" }),
    copy("order-archive", "ap", "order-events", "eu", { leader: "", lag: undefined, lastActiveMs: undefined }),
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

  it("shows each server's memory and JetStream share, and the memory finding beside the chip", async () => {
    vi.mocked(requests.getTopology).mockResolvedValue(followups());
    renderApp(<Topology />, "/topology");
    const map = await region(/Map of clusters/);
    const plain = await map.findByRole("link", { name: "nats-eu-0, memory 100 MiB; JetStream memory 0 B of 2 GiB" });
    expect(plain).not.toHaveClass("warn");
    const shown = (el: HTMLElement) => [...el.querySelectorAll(".tm-mem")].map((e) => e.textContent);
    expect(shown(plain)).toEqual(["100 MiB"]);
    // The JetStream share is visible when the store holds anything.
    const js = map.getByRole("link", { name: "nats-eu-2, memory 120 MiB; JetStream memory 80 MiB of 2 GiB" });
    expect(shown(js)).toEqual(["120 MiB", "JS 4%"]);
    const high = map.getByRole("link", { name: "nats-eu-3, memory 900 MiB; JetStream memory 0 B of 2 GiB, server-008 fires" });
    expect(high).toHaveClass("warn");
    const finding = map.getByRole("link", { name: "server-008" });
    expect(finding).toHaveAttribute("href", "/findings/server-008%3Anats-eu-3");
    expect(finding).toHaveClass("tp-warn");
    // A muted finding is linked, not in the warning colour.
    expect(map.getByRole("link", { name: /^nats-eu-1, memory 110 MiB/ })).not.toHaveClass("warn");
    const quietLink = map.getByRole("link", { name: /^server-007/ });
    expect(quietLink).toHaveTextContent("server-007 (muted)");
    expect(quietLink).not.toHaveClass("tp-warn");
    expect(map.getByRole("link", { name: "nats-ap-0" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Capacity" })).toHaveAttribute("href", "/capacity");
  });

  it("lists mirrors and sources between clusters, those with a finding first, and notes them on the cluster", async () => {
    const errors = vi.spyOn(console, "error").mockImplementation(() => {});
    vi.mocked(requests.getTopology).mockResolvedValue(followups());
    renderApp(<Topology />, "/topology");
    const table = await screen.findByRole("region", { name: "Replication between clusters" });
    // Two sources of one origin keep keys of their own.
    expect(errors.mock.calls.filter((c) => String(c[0]).includes("same key"))).toEqual([]);
    errors.mockRestore();
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveClass("warn");
    expect(within(rows[0]).getByRole("rowheader")).toHaveTextContent(
      "catalog-changes on us → search-index on apstream-022: Leader nats-ap-0: catalog-changes: not heard from its origin for 3m.",
    );
    expect(within(rows[0]).getByRole("link", { name: "stream-022" })).toHaveAttribute("href", "/findings/stream-022%3AAPP%2Fsearch-index");
    expect(within(rows[0]).getAllByRole("cell").map((c) => c.textContent)).toEqual(["Source", "18,400", "800 ms ago", "APP"]);
    // A second source of the same origin, told apart by its filter.
    expect(rows[1]).not.toHaveClass("warn");
    expect(within(rows[1]).getByRole("rowheader")).toHaveTextContent("filter catalog.eu.>");
    // Without the leader's report.
    expect(within(rows[2]).getAllByRole("cell")[1]).toHaveTextContent(/^– leader not reported$/);
    expect(within(rows[2]).getByRole("link", { name: "order-events" })).toHaveAttribute("href", "/streams/APP/order-events");
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
  it("words when a copy last heard", () => {
    expect(lastHeard(copy("a", "ap", "b", "eu", { lastActiveMs: 1200 }))).toBe("1.2 s ago");
    expect(lastHeard(copy("a", "ap", "b", "eu", { lastActiveMs: 190_000 }))).toBe("3m ago");
    expect(lastHeard(copy("a", "ap", "b", "eu", { neverHeard: true, lastActiveMs: undefined }))).toBe("never");
    expect(lastHeard(copy("a", "ap", "b", "eu", { lastActiveMs: undefined }))).toBeUndefined();
  });

  it("gives the JetStream share at least 1% when the store holds anything, and nothing without a reading", () => {
    const s = { name: "n", missing: false, metaLeader: false, memBytes: 100 * MIB, jsMemoryBytes: 1 * MIB, jsMaxMemoryBytes: 2048 * MIB, memoryFindingId: "", memoryFindingCode: "", memoryFindingQuiet: false };
    expect(serverMemory(s)).toEqual({ text: "100 MiB", js: { short: "JS 1%", text: "JetStream memory 1 MiB of 2 GiB" }, warn: undefined });
    expect(serverMemory({ ...s, jsMemoryBytes: 0 }).js).toEqual({ text: "JetStream memory 0 B of 2 GiB" });
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

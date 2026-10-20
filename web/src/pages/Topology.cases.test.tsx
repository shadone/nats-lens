// Edge cases of the Topology page: the second review's cases and the fixes
// that followed them.
import { screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as requests from "../api/requests";
import { LeafTable } from "../components/TopologyTables";
import { TopologyMap } from "../components/TopologyMap";
import { fmtShortDuration } from "../lib/format";
import { boxCentre, clusterNote, farLabels, gatewayLabelParts, labelWidth, rowFractions, routesCluster, summary } from "../lib/topologyView";
import { cluster, gateway, global, leaf, route } from "../test/fixtures/topology";
import { renderApp } from "../test/render";
import { Topology } from "./Topology";

vi.mock("../api/requests", () => ({ getTopology: vi.fn(), getLensInfo: vi.fn(() => new Promise(() => {})), getShellStatus: vi.fn(() => new Promise(() => {})) }));

describe("routes cluster across refetches", () => {
  it("stays on one cluster when two clusters have slow routes and their order swaps", () => {
    const a = global();
    a.routes = [route("eu", "nats-eu-0", "nats-eu-1", { rttMs: 4.12, slow: true }), route("us", "nats-us-0", "nats-us-1", { rttMs: 4.1, slow: true })];
    const b = global();
    b.routes = [route("us", "nats-us-0", "nats-us-1", { rttMs: 4.13, slow: true }), route("eu", "nats-eu-0", "nats-eu-1", { rttMs: 4.1, slow: true })];
    expect(routesCluster(b, null)).toBe(routesCluster(a, null));
    expect(routesCluster(a, null)).toBe("eu");
    // Without the meta leader among them, the first by name.
    for (const t of [a, b]) t.clusters = t.clusters.map((c) => ({ ...c, metaLeader: false }));
    expect([routesCluster(a, null), routesCluster(b, null)]).toEqual(["eu", "eu"]);
  });
});

describe("fmtShortDuration edges", () => {
  it.each([
    [0.0049, "<0.01 ms"],
    [0.005, "0.01 ms"],
    [0.995, "0.99 ms"],
    [9.95, "9.9 ms"],
    [999.49, "999 ms"],
    [999.5, "1 s"],
    [59_960, "60 s"],
  ])("%s -> %s", (v, s) => expect(fmtShortDuration(v)).toBe(s));
});

describe("down pair and summary", () => {
  it("labels a far down pair", () => {
    expect(gatewayLabelParts(gateway("ap", "us", 0, { connections: 0, expected: 4, rttMedianMs: undefined, rttMaxMs: undefined }))).toEqual(["gateway down", "0 of 4 up"]);
  });
  it("names a down pair once in the note, not also dropping", () => {
    const t = global();
    t.gateways = [gateway("ap", "us", 0, { connections: 0, expected: 4, drops1h: 2, droppingServers: ["nats-ap-1"], rttMedianMs: undefined })];
    expect(clusterNote(t.clusters[0], t.gateways)).toBe("4 streams led · gateway to us down");
    expect(summary(t)).toMatch(/^3 clusters, every gateway pair down/);
  });
});

describe("leaf rows wording", () => {
  function row(l: ReturnType<typeof leaf>, failed: string[] = []) {
    renderApp(<LeafTable rows={[l]} total={1} at={Date.parse("2026-10-17T14:30:00Z")} leafzFailed={{ [l.hubCluster]: failed }} />);
    return within(screen.getAllByRole("row")[1]);
  }
  it("not connected without a last-seen time names the last hub", () => {
    const r = row(leaf("pickup-1", "nats-eu-1", { connected: false, lastSeen: undefined }));
    expect(r.getByRole("rowheader")).toHaveTextContent("pickup-1not connected; last hub nats-eu-1");
  });
  it("unknown state without a failed server list or time", () => {
    const r = row(leaf("pickup-2", "nats-eu-1", { connected: false, connectionUnknown: true, lastSeen: undefined }));
    expect(r.getByRole("rowheader")).toHaveTextContent("pickup-2state unknown; last hub nats-eu-1");
  });
  it("keeps every server name in the note unbroken", () => {
    const r = row(leaf("pickup-3", "nats-us-1", { connected: false, connectionUnknown: true, lastSeen: Date.parse("2026-10-17T09:12:00Z") }), ["nats-us-4", "nats-us-5"]);
    const names = [...r.getByRole("rowheader").querySelectorAll("small .tm-nw")].map((e) => e.textContent);
    expect(names).toEqual(["nats-us-4", "nats-us-5", "nats-us-1"]);
    expect(r.getByRole("rowheader")).toHaveTextContent("state unknown: LEAFZ did not answer on nats-us-4, nats-us-5; last seen 09:12 on nats-us-1");
  });
  it("lets a list of hubs wrap between names, each name whole", () => {
    const r = row(leaf("warehouse-eu-03", "nats-eu-5", { hubServers: ["nats-eu-5", "nats-eu-6", "nats-eu-7"] }));
    const hub = r.getAllByRole("cell")[0];
    expect(hub).toHaveClass("tp-hubs");
    expect([...hub.querySelectorAll(".tm-nw")].map((e) => e.textContent)).toEqual(["nats-eu-5,", "nats-eu-6,", "nats-eu-7"]);
  });
  it("marks a row of unknown state with the topology's own class", () => {
    const r = row(leaf("pickup-2", "nats-eu-1", { connected: false, connectionUnknown: true }));
    expect(r.getByRole("rowheader").closest("tr")).toHaveClass("tp-unknown");
  });
});

describe("gateway labels", () => {
  it("starts a wrapped line with the separator, never ends one with it", () => {
    renderApp(<TopologyMap t={global()} width={1110} onPick={() => {}} />);
    const label = [...document.querySelectorAll(".tm-gw-label")].find((e) => e.textContent?.includes("drops/h"))!;
    expect([...label.querySelectorAll("[aria-hidden] .tm-nw")].map((e) => e.textContent)).toEqual(["gateway 212 ms", "· 3 drops/h"]);
  });

  it("gives a 3-cluster map at 1440 px a gap that holds a label with drops on one line", () => {
    const f = rowFractions(3);
    // The map's inner width at a 1440 px window.
    const gap = (1110 / (f.slots * f.box + (f.slots - 1) * f.gap)) * f.gap;
    expect(gap).toBeGreaterThanOrEqual(labelWidth("gateway 84 ms · 1 drop/h"));
  });

  it("places a far label right of its bracket's leg, wrapped to end before the next line it would cross", () => {
    const t = global();
    t.clusters = [...t.clusters, cluster("", ["nats-x-0"], { leafConnected: 2, leafNotConnected: 0 })];
    t.gateways = [
      gateway("", "ap", 231, { connections: 5, expected: 6, drops1h: 2, droppingServers: ["nats-x-1"] }),
      gateway("ap", "us", 0, { connections: 0, expected: 14, rttMedianMs: undefined, rttMaxMs: undefined }),
    ];
    const order = t.clusters.map((c) => c.name);
    const leaves = t.clusters.map((c) => (c.leafConnected ?? 0) + (c.leafNotConnected ?? 0) > 0);
    const placed = farLabels(order, t.gateways, leaves, 1110);
    expect(placed).toHaveLength(2);
    for (const p of placed) {
      expect(p.left).toBeGreaterThan(p.legX);
      expect(p.left + p.width).toBeLessThanOrEqual(p.limit);
      // It ends before every leaf node connector between the bracket's boxes.
      for (let k = p.b.i + 1; k < p.b.j; k++) if (leaves[k]) expect(p.left + p.width).toBeLessThan(boxCentre(k, order.length, 1110));
      expect(p.lines.join(" ")).toBe(gatewayLabelParts(p.b.g).map((x, k) => (k > 0 ? `· ${x}` : x)).join(" "));
    }
    // Labels on different levels never overlap vertically.
    const [a, b] = [...placed].sort((x, y) => x.y - y.y);
    expect(a.y + a.height / 2).toBeLessThanOrEqual(b.y - b.height / 2);
  });
});

describe("round 3", () => {
  it("names only the failed LEAFZ servers of the site's own cluster", async () => {
    const t = global();
    t.unobserved = [{ endpoint: "LEAFZ", servers: ["nats-eu-3", "nats-us-2"] }];
    t.leafnodes.splice(1, 0, { ...t.leafnodes[0], name: "pickup-1210", hubServer: "nats-us-1", hubCluster: "us", connectionUnknown: true });
    vi.mocked(requests.getTopology).mockResolvedValue(t);
    renderApp(<Topology />, "/topology");
    const header = await screen.findByRole("rowheader", { name: /pickup-1210/ });
    expect(header).toHaveTextContent("state unknown: LEAFZ did not answer on nats-us-2; last seen");
    expect(header).not.toHaveTextContent("nats-eu-3");
  });

  it("puts the unknown cluster after named ones among clusters with slow routes", () => {
    const t = global();
    t.clusters = [...t.clusters.map((c) => ({ ...c, metaLeader: false })), cluster("", ["nats-x-0", "nats-x-1"])];
    t.routes = [route("", "nats-x-0", "nats-x-1", { rttMs: 5, slow: true }), route("us", "nats-us-0", "nats-us-1", { rttMs: 4, slow: true })];
    expect(routesCluster(t, null)).toBe("us");
    // Alone, it is still picked.
    t.routes = [route("", "nats-x-0", "nats-x-1", { rttMs: 5, slow: true })];
    expect(routesCluster(t, null)).toBe("");
  });
});

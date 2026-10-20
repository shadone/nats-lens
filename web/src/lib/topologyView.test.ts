import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { GetTopologyResponseSchema } from "../gen/natslens/v1/topology_pb";
import { topology } from "../api/topology";
import { gateway, global } from "../test/fixtures/topology";
import { brackets, gatewayLabel, leafBox, neighbours, routesCluster, rowLayout } from "./topologyView";

describe("topology adapter", () => {
  it("keeps unset values undefined and reads epoch 0 as no collection", () => {
    const t = topology(
      create(GetTopologyResponseSchema, {
        epoch: 0n,
        routes: [{ cluster: "eu", serverA: "a", serverB: "b" }],
        leafnodes: [{ name: "site", connected: false }],
        gateways: [{ clusterA: "eu", clusterB: "us", rttMedian: { seconds: 0n, nanos: 420_000 } }],
      }),
    );
    expect(t.collected).toBe(false);
    expect(t.routes[0]).toMatchObject({ rttMs: undefined, pendingBytes: undefined, pool: undefined });
    expect(t.leafnodes[0]).toMatchObject({ rttMs: undefined, subscriptions: undefined, drops24h: undefined, lastSeen: undefined });
    expect(t.gateways[0]).toMatchObject({ rttMedianMs: 0.42, rttMaxMs: undefined, drops1h: undefined, pendingBytes: undefined });
  });
});

describe("map layout", () => {
  it("places far gateways on levels that do not overlap, the deepest leg nearest each centre", () => {
    const order = ["a", "b", "c", "d"];
    const b = brackets(order, [gateway("a", "c", 1), gateway("b", "d", 1), gateway("a", "d", 1), gateway("a", "b", 1)]);
    const by = Object.fromEntries(b.map((x) => [`${x.g.a}${x.g.b}`, x]));
    expect(Object.keys(by).sort()).toEqual(["ac", "ad", "bd"]);
    expect([by.ac.level, by.bd.level, by.ad.level]).toEqual([0, 1, 2]);
    // On a, ad is deeper than ac, so its leg is nearer the centre.
    expect([by.ad.legI, by.ac.legI]).toEqual([0, 1]);
    expect([by.ad.legJ, by.bd.legJ]).toEqual([0, 1]);
    expect([...neighbours(order, [gateway("b", "a", 1)]).keys()]).toEqual([0]);
  });

  it("draws the row for up to four clusters wide enough, else the grid", () => {
    expect(rowLayout(3, 0)).toBe(true);
    expect(rowLayout(3, 1100)).toBe(true);
    expect(rowLayout(3, 390)).toBe(false);
    // A 1010 px window, whose map is about 680 px inside the card: the label "gateway 166 ms" would wrap in a gap under 116 px.
    expect(rowLayout(3, 680)).toBe(false);
    expect(rowLayout(4, 1110)).toBe(true);
    expect(rowLayout(4, 900)).toBe(false);
    expect(rowLayout(5, 2000)).toBe(false);
  });

  it("words gateways, leaf boxes and the default cluster", () => {
    expect(gatewayLabel(gateway("a", "b", 84, { connections: 4, expected: 6 }))).toBe("gateway 84 ms · 4 of 6 up");
    expect(gatewayLabel(gateway("a", "b", 84, { rttMedianMs: undefined }))).toBe("gateway");
    const t = global();
    expect(leafBox(t.clusters[0])).toBeUndefined();
    expect(leafBox(t.clusters[1])).toEqual({ sites: 3, unit: "sites", state: "1 not connected" });
    expect(routesCluster(t, null)).toBe("eu");
    expect(routesCluster(t, "us")).toBe("us");
    expect(routesCluster(t, "gone")).toBe("eu");
  });
});

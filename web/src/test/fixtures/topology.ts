// Topology view models for tests: the shop's three clusters with a dropping
// gateway, a slow route and a leaf node site not connected, and a single
// cluster with routes only.
import type { Topology, TopologyCluster, TopologyGateway, TopologyLeafnode, TopologyRoute } from "../../api/topology";

export const EPOCH_AT = Date.parse("2026-10-17T14:30:00Z");

export function cluster(name: string, servers: string[], o: Partial<TopologyCluster> & { missing?: string[] } = {}): TopologyCluster {
  const { missing = [], ...rest } = o;
  return {
    name,
    place: "",
    servers: servers.map((s) => ({ name: s, missing: missing.includes(s), metaLeader: false })),
    metaLeader: false,
    ...rest,
  };
}

export function gateway(a: string, b: string, rttMs: number, o: Partial<TopologyGateway> = {}): TopologyGateway {
  return { a, b, connections: 6, expected: 6, rttMaxMs: rttMs + 0.5, rttMedianMs: rttMs, pendingBytes: 0, drops1h: 0, drops24h: 0, droppingServers: [], ...o };
}

export function route(cluster: string, a: string, b: string, o: Partial<TopologyRoute> = {}): TopologyRoute {
  return { cluster, a, b, rttMs: 0.4, pendingBytes: 0, pool: 3, pinnedAccounts: [], oneSided: false, slow: false, reportedBy: [a, b], ...o };
}

export function leaf(name: string, hub: string, o: Partial<TopologyLeafnode> = {}): TopologyLeafnode {
  return {
    name,
    account: "APP",
    accountName: "",
    hubServer: hub,
    hubCluster: hub.split("-")[1] ?? "",
    connected: true,
    rttMs: 21,
    subscriptions: 148,
    drops24h: 0,
    compression: "s2_auto",
    hubServers: o.connected === false ? [] : [hub],
    connectionUnknown: false,
    ...o,
  };
}

/** eu, us and ap; ap–us dropped 3 times in the last hour from nats-ap-1; nats-us-2 is missing. */
export function global(): Topology {
  return {
    collected: true,
    epochAt: EPOCH_AT,
    clusters: [
      cluster("ap", ["nats-ap-0", "nats-ap-1", "nats-ap-2"], { place: "ap", streamsLed: 4, leafConnected: 0, leafNotConnected: 0 }),
      cluster("eu", ["nats-eu-0", "nats-eu-1", "nats-eu-2", "nats-eu-3"], { place: "eu", metaLeader: true, streamsLed: 5, leafConnected: 2, leafNotConnected: 1 }),
      cluster("us", ["nats-us-0", "nats-us-1", "nats-us-2"], { place: "us", streamsLed: 6, leafConnected: 1, leafNotConnected: 0, missing: ["nats-us-2"] }),
    ],
    gateways: [
      gateway("ap", "us", 212, { drops1h: 3, drops24h: 7, droppingServers: ["nats-ap-1"] }),
      gateway("ap", "eu", 166),
      gateway("eu", "us", 84, { drops24h: 4 }),
    ],
    routes: [
      route("eu", "nats-eu-0", "nats-eu-3", { rttMs: 4.1, slow: true }),
      route("eu", "nats-eu-0", "nats-eu-1", { rttMs: 0.5, pinnedAccounts: ["APP"] }),
      route("eu", "nats-eu-1", "nats-eu-2", { rttMs: undefined, pendingBytes: undefined, pool: undefined, oneSided: true, reportedBy: ["nats-eu-1"] }),
      route("us", "nats-us-0", "nats-us-1", { rttMs: 0.3 }),
    ],
    leafnodes: [
      leaf("pickup-0417", "nats-eu-1", { connected: false, rttMs: undefined, subscriptions: undefined, drops24h: 17, lastSeen: Date.parse("2026-10-17T09:12:00Z") }),
      leaf("warehouse-eu-03", "nats-eu-2", { drops24h: 2 }),
      leaf("pickup-1208", "nats-us-1", { rttMs: 65, subscriptions: undefined, drops24h: undefined }),
      leaf("pickup-0122", "nats-eu-0", { rttMs: 12, subscriptions: 1904 }),
    ],
    leafnodesTotal: 4,
    unobserved: [],
    dropsWindowStart: EPOCH_AT - 86_400_000,
    collectIntervalMs: 30_000,
  };
}

/** One cluster of three servers with routes only. */
export function single(): Topology {
  return {
    collected: true,
    epochAt: EPOCH_AT,
    clusters: [cluster("example-prod", ["nats-0", "nats-1", "nats-2"], { metaLeader: true, streamsLed: 13, leafConnected: 0, leafNotConnected: 0 })],
    gateways: [],
    routes: [route("example-prod", "nats-1", "nats-2", { rttMs: 0.58 }), route("example-prod", "nats-0", "nats-1"), route("example-prod", "nats-0", "nats-2")],
    leafnodes: [],
    leafnodesTotal: 0,
    unobserved: [],
    dropsWindowStart: EPOCH_AT - 86_400_000,
    collectIntervalMs: 30_000,
  };
}

export function empty(): Topology {
  return { collected: false, clusters: [], gateways: [], routes: [], leafnodes: [], leafnodesTotal: 0, unobserved: [] };
}

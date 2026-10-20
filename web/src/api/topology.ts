// Topology: GetTopologyResponse to the view model of the Topology page.
// The only file that reads the topology messages; a value the server left
// unset stays undefined here, never 0.
import type { GetTopologyResponse } from "../gen/natslens/v1/topology_pb";
import type { Duration } from "@bufbuild/protobuf/wkt";
import { dur, ms, optN } from "./adapt-common";

/** A round trip in milliseconds with its fraction: durationMs rounds to whole milliseconds, and routes take 0.4 ms. */
function rtt(d: Duration | undefined): number | undefined {
  return d ? Number(d.seconds) * 1000 + d.nanos / 1e6 : undefined;
}

export interface TopologyServer {
  name: string;
  /** Expected in the collection and answered nothing. */
  missing: boolean;
  metaLeader: boolean;
}

export interface TopologyCluster {
  name: string;
  place: string;
  servers: TopologyServer[];
  metaLeader: boolean;
  streamsLed?: number;
  leafConnected?: number;
  leafNotConnected?: number;
}

export interface TopologyGateway {
  a: string;
  b: string;
  connections: number;
  expected: number;
  rttMaxMs?: number;
  rttMedianMs?: number;
  pendingBytes?: number;
  drops1h?: number;
  drops24h?: number;
  droppingServers: string[];
}

export interface TopologyRoute {
  cluster: string;
  a: string;
  b: string;
  rttMs?: number;
  pendingBytes?: number;
  /** Pooled routes; undefined when every route of the pair is pinned. */
  pool?: number;
  pinnedAccounts: string[];
  oneSided: boolean;
  slow: boolean;
  reportedBy: string[];
}

export interface TopologyLeafnode {
  name: string;
  account: string;
  accountName: string;
  hubServer: string;
  hubCluster: string;
  connected: boolean;
  rttMs?: number;
  subscriptions?: number;
  drops24h?: number;
  lastSeen?: number;
  compression: string;
}

export interface TopologyUnobserved {
  endpoint: string;
  servers: string[];
}

export interface Topology {
  /** False when the range holds no collection; everything else is then empty. */
  collected: boolean;
  epochAt?: number;
  clusters: TopologyCluster[];
  gateways: TopologyGateway[];
  routes: TopologyRoute[];
  leafnodes: TopologyLeafnode[];
  /** Sites before the server's limit; more than leafnodes.length when rows were cut. */
  leafnodesTotal: number;
  unobserved: TopologyUnobserved[];
  dropsWindowStart?: number;
  collectIntervalMs?: number;
}

export function topology(r: GetTopologyResponse): Topology {
  return {
    collected: r.epoch !== 0n,
    epochAt: ms(r.epochAt),
    clusters: r.clusters.map((c) => ({
      name: c.name,
      place: c.place,
      servers: c.servers.map((s) => ({ name: s.name, missing: s.missing, metaLeader: s.metaLeader })),
      metaLeader: c.metaLeader,
      streamsLed: c.streamsLed,
      leafConnected: c.leafnodesConnected,
      leafNotConnected: c.leafnodesNotConnected,
    })),
    gateways: r.gateways.map((g) => ({
      a: g.clusterA,
      b: g.clusterB,
      connections: g.connections,
      expected: g.expected,
      rttMaxMs: rtt(g.rttMax),
      rttMedianMs: rtt(g.rttMedian),
      pendingBytes: optN(g.pendingBytes),
      drops1h: g.drops1h,
      drops24h: g.drops24h,
      droppingServers: g.droppingServers,
    })),
    routes: r.routes.map((x) => ({
      cluster: x.cluster,
      a: x.serverA,
      b: x.serverB,
      rttMs: rtt(x.rtt),
      pendingBytes: optN(x.pendingBytes),
      pool: x.pool,
      pinnedAccounts: x.pinnedAccounts,
      oneSided: x.oneSided,
      slow: x.slow,
      reportedBy: x.reportedBy,
    })),
    leafnodes: r.leafnodes.map((l) => ({
      name: l.name,
      account: l.account,
      accountName: l.accountName,
      hubServer: l.hubServer,
      hubCluster: l.hubCluster,
      connected: l.connected,
      rttMs: rtt(l.rtt),
      subscriptions: optN(l.subscriptions),
      drops24h: l.drops24h,
      lastSeen: ms(l.lastSeen),
      compression: l.compression,
    })),
    leafnodesTotal: r.leafnodesTotal,
    unobserved: r.unobserved.map((u) => ({ endpoint: u.endpoint, servers: u.servers })),
    dropsWindowStart: ms(r.dropsWindowStart),
    collectIntervalMs: dur(r.collectInterval),
  };
}

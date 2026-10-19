// The placement matrix: proto to view model, and the filters it offers.
import { PlacementFilter, type GetPlacementResponse, type PlacementAsset as PAsset, type PlacementCell as PCell } from "../gen/natslens/v1/placement_pb";
import { ServerRole, type ServerSummary } from "../gen/natslens/v1/servers_pb";
import { dur, entityOrEmpty, ms, optN } from "./adapt-common";
import { placementState, type PlacementStateName } from "./adapt-placement";
import type { Entity } from "./model";

/** A filter of the matrix, as the URL names it. */
export type MatrixFilter = "all" | "disagree" | "behind" | "missing" | "single";

export const MATRIX_FILTERS: { id: MatrixFilter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "disagree", label: "Servers disagree" },
  { id: "behind", label: "Replica behind" },
  { id: "missing", label: "Missing somewhere" },
  { id: "single", label: "Single replica" },
];

export const FILTER_ENUM: Record<MatrixFilter, PlacementFilter> = {
  all: PlacementFilter.UNSPECIFIED,
  disagree: PlacementFilter.SERVERS_DISAGREE,
  behind: PlacementFilter.REPLICA_BEHIND,
  missing: PlacementFilter.MISSING,
  single: PlacementFilter.SINGLE_REPLICA,
};

/** The filter a URL value names; all for none or an unknown one. */
export function matrixFilter(v: string | null): MatrixFilter {
  return MATRIX_FILTERS.find((f) => f.id === v)?.id ?? "all";
}

/** A column of the matrix: one server. */
export interface MatrixServer {
  name: string;
  /** Streams and consumers it leads; unset when not reported. */
  leads?: number;
  memBytes?: number;
  uptimeMs?: number;
  metaLeader: boolean;
  /** Whether it answered in the collection read. */
  present: boolean;
}

/** One server's view of an asset. */
export interface MatrixCell {
  server: string;
  state: PlacementStateName;
  created?: number;
  configHash: string;
  lag?: number;
  messages?: number;
  firstSeq?: number;
  lastSeq?: number;
  delivered?: number;
  ackFloor?: number;
  /** Settings on which it differs from most servers: created, config_hash. */
  differs: string[];
  /** The other replicas it names. */
  peers: string[];
}

export interface MatrixAsset {
  entity: Entity;
  replicas?: number;
  leader: string;
  leaderSince?: number;
  cells: MatrixCell[];
  /** Configured replicas no server names. */
  unplaced: number;
  serversDisagree: boolean;
  messages?: number;
  durable: boolean;
  /** Whether it passes the filter; a stream may be listed for its consumers only. */
  matches: boolean;
  consumers: MatrixAsset[];
}

export interface Matrix {
  servers: MatrixServer[];
  streams: MatrixAsset[];
  counts: Record<MatrixFilter, number>;
  /** When the collection read started; unset when the range holds none. */
  epochAt?: number;
}

function server(s: ServerSummary): MatrixServer {
  const leads = s.streamLeaders === undefined && s.consumerLeaders === undefined ? undefined : (s.streamLeaders ?? 0) + (s.consumerLeaders ?? 0);
  return { name: s.name, leads, memBytes: optN(s.memBytes), uptimeMs: dur(s.uptime), metaLeader: s.role === ServerRole.META_LEADER, present: s.present };
}

function cell(c: PCell): MatrixCell {
  return {
    server: c.server,
    state: placementState(c.state),
    created: ms(c.created),
    configHash: c.configHash,
    lag: optN(c.lag),
    messages: optN(c.messages),
    firstSeq: optN(c.firstSeq),
    lastSeq: optN(c.lastSeq),
    delivered: optN(c.deliveredStreamSeq),
    ackFloor: optN(c.ackFloorStreamSeq),
    differs: c.differs,
    peers: c.peers,
  };
}

function asset(a: PAsset): MatrixAsset {
  return {
    entity: entityOrEmpty(a.entity),
    replicas: a.replicas,
    leader: a.leader,
    leaderSince: ms(a.leaderSince),
    cells: a.cells.map(cell),
    unplaced: a.unplaced,
    serversDisagree: a.serversDisagree,
    messages: optN(a.messages),
    durable: a.durable,
    matches: a.matches,
    consumers: a.consumers.map(asset),
  };
}

export function matrix(r: GetPlacementResponse): Matrix {
  const c = r.counts;
  return {
    servers: r.servers.map(server),
    streams: r.streams.map(asset),
    counts: { all: c?.all ?? 0, disagree: c?.serversDisagree ?? 0, behind: c?.replicaBehind ?? 0, missing: c?.missing ?? 0, single: c?.singleReplica ?? 0 },
    epochAt: ms(r.epochAt),
  };
}

/** Every asset of a matrix: each stream, then its consumers. */
export function assetsOf(m: Matrix): MatrixAsset[] {
  return m.streams.flatMap((s) => [s, ...s.consumers]);
}

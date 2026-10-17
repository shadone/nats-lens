// Proto to view model: overview, servers, streams, consumers, connections.
import type { ConsumerServerView, ConsumerSummary as PConsumer, GetConsumerResponse } from "../gen/natslens/v1/consumers_pb";
import type { GetOverviewResponse, Tile as PTile } from "../gen/natslens/v1/overview_pb";
import {
  ServerRole as PRole,
  type Connection as PConnection,
  type GetServerResponse,
  type ServerSummary,
} from "../gen/natslens/v1/servers_pb";
import type { GetStreamResponse, StreamServerView, StreamSummary as PStream } from "../gen/natslens/v1/streams_pb";
import {
  config,
  disagreements,
  dur,
  entityOrEmpty,
  freshness,
  level,
  ms,
  optN,
  range,
  series,
  unit,
  window,
} from "./adapt-common";
import { finding, optFinding, tally } from "./adapt-findings";
import type {
  Connection,
  ConsumerDetail,
  ConsumerSummary,
  Overview,
  Server,
  ServerDetail,
  ServerRole,
  StreamDetail,
  StreamSummary,
  Tile,
} from "./model";

const ROLE: Record<PRole, ServerRole> = {
  [PRole.UNSPECIFIED]: "unknown",
  [PRole.META_LEADER]: "meta leader",
  [PRole.FOLLOWER]: "follower",
  [PRole.NO_JETSTREAM]: "no JetStream",
};

export function server(s: ServerSummary): Server {
  return {
    name: s.name,
    serverId: s.serverId,
    version: s.version,
    cluster: s.cluster,
    startTime: ms(s.startTime),
    uptimeMs: dur(s.uptime),
    role: ROLE[s.role] ?? "unknown",
    present: s.present,
    healthz: s.healthz,
    healthzError: s.healthzError,
    restarts: s.restarts.map((t) => ms(t) ?? 0),
    connections: optN(s.connections),
    jsStorageBytes: optN(s.jsStorageBytes),
    jsMaxStorageBytes: optN(s.jsMaxStorageBytes),
    memBytes: optN(s.memBytes),
    cpuPercent: optN(s.cpuPercent),
    status: level(s.status),
  };
}

function tile(t: PTile): Tile {
  return {
    id: t.id,
    label: t.label,
    value: optN(t.value),
    unit: unit(t.unit),
    valueSuffix: t.valueSuffix,
    value2: optN(t.value2),
    value2Suffix: t.value2Suffix,
    subtitle: t.subtitle,
    status: level(t.status),
    series: series(t.series),
    series2: series(t.series2),
    limit: optN(t.limit),
    description: t.description || undefined,
    valueText: t.valueText || undefined,
  };
}

export function overview(r: GetOverviewResponse): Overview {
  return {
    cluster: r.cluster,
    range: range(r.range),
    freshness: freshness(r.freshness),
    servers: r.servers.map(server),
    tally: tally(r.tally),
    worst: optFinding(r.worstFinding),
    tiles: r.tiles.map(tile),
    windows: r.windows.map(window),
  };
}

export function serverDetail(r: GetServerResponse): ServerDetail {
  return {
    server: r.server ? server(r.server) : undefined,
    endpoints: r.endpoints.map((e) => ({ endpoint: e.endpoint, state: e.state, error: e.error })),
    processes: r.processes.map((p) => ({
      serverId: p.serverId,
      startTime: ms(p.startTime),
      firstSeen: ms(p.firstSeen),
      lastSeen: ms(p.lastSeen),
    })),
    subscriptions: optN(r.subscriptions),
    slowConsumers: optN(r.slowConsumers),
    routes: optN(r.routes),
    jsStreams: optN(r.jsStreams),
    jsConsumers: optN(r.jsConsumers),
    metaLeader: r.metaLeader,
    configDigest: r.configDigest,
  };
}

export function stream(s: PStream): StreamSummary {
  return {
    entity: entityOrEmpty(s.entity),
    replicas: optN(s.replicas),
    messages: optN(s.messages),
    bytes: optN(s.bytes),
    consumers: optN(s.consumers),
    consumersExpected: optN(s.consumersExpected),
    bytesPerDay: optN(s.bytesPerDay),
    bytes7d: series(s.bytes7d),
    status: level(s.status),
    findingCodes: s.findingCodes,
    serversDisagree: s.serversDisagree,
    leader: s.leader,
  };
}

function streamServer(v: StreamServerView) {
  return {
    server: v.server,
    isLeader: v.isLeader,
    current: v.current,
    offline: v.offline,
    lag: optN(v.lag),
    messages: optN(v.messages),
    bytes: optN(v.bytes),
    firstSeq: optN(v.firstSeq),
    lastSeq: optN(v.lastSeq),
    created: ms(v.created),
    configHash: v.configHash,
  };
}

export function consumer(c: PConsumer): ConsumerSummary {
  return {
    entity: entityOrEmpty(c.entity),
    durable: c.durable,
    replicas: optN(c.replicas),
    // Unset means not observed (no leader); shown as a dash, never as 0.
    // The other two are not optional in the API, so a missing consumer
    // leaves them out too.
    numPending: optN(c.numPending),
    numAckPending: c.present ? optN(c.numAckPending) : undefined,
    numRedelivered: c.present ? optN(c.numRedelivered) : undefined,
    numWaiting: optN(c.numWaiting),
    leader: c.leader,
    lastDelivered: ms(c.lastDelivered),
    paused: c.paused,
    status: level(c.status),
    findingCodes: c.findingCodes,
    serversDisagree: c.serversDisagree,
    present: c.present,
  };
}

function consumerServer(v: ConsumerServerView) {
  return {
    server: v.server,
    isLeader: v.isLeader,
    current: v.current,
    offline: v.offline,
    lag: optN(v.lag),
    deliveredStreamSeq: optN(v.deliveredStreamSeq),
    ackFloorStreamSeq: optN(v.ackFloorStreamSeq),
    created: ms(v.created),
    configHash: v.configHash,
  };
}

export function streamDetail(r: GetStreamResponse): StreamDetail {
  return {
    stream: r.stream ? stream(r.stream) : undefined,
    config: config(r.config),
    servers: r.servers.map(streamServer),
    disagreements: disagreements(r.disagreements),
    consumers: r.consumers.map(consumer),
    findings: r.findings.map(finding),
  };
}

export function consumerDetail(r: GetConsumerResponse): ConsumerDetail {
  return {
    consumer: r.consumer ? consumer(r.consumer) : undefined,
    config: config(r.config),
    servers: r.servers.map(consumerServer),
    disagreements: disagreements(r.disagreements),
    findings: r.findings.map(finding),
  };
}

export function connection(c: PConnection): Connection {
  const cid = String(c.cid);
  return {
    key: c.key || `${c.server}/${cid}`,
    server: c.server,
    cid,
    name: c.name,
    lang: c.lang,
    version: c.version,
    account: c.account,
    accountName: c.accountName || undefined,
    tooling: c.tooling,
    user: c.user,
    ip: c.ip,
    port: optN(c.port),
    startTime: ms(c.startTime),
    lastActivity: ms(c.lastActivity),
    rttMs: dur(c.rtt),
    pendingBytes: optN(c.pendingBytes),
    inMsgs: optN(c.inMsgs),
    outMsgs: optN(c.outMsgs),
    inBytes: optN(c.inBytes),
    outBytes: optN(c.outBytes),
    stalls: optN(c.stalls),
    subscriptions: optN(c.subscriptions),
  };
}

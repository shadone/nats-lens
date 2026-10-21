// View models: what the pages and components read. Only src/api/adapt*.ts
// and src/api/requests.ts touch the generated protobuf types, so a change in
// the protos is a change there. Times are epoch milliseconds (UTC), IDs are
// strings, and an unset number is undefined (shown as a dash, never as 0).

/** A level the reference draws: badge, glyph, cell, health icon. */
export type Level = "critical" | "warning" | "info" | "muted" | "ok" | "nodata";
export type SeverityLevel = "critical" | "warning" | "info";

export type EntityKind = "system" | "server" | "account" | "stream" | "consumer" | "connection" | "client" | "unknown";

export interface Entity {
  kind: EntityKind;
  key: string;
  name: string;
  account: string;
  /** Readable account name when known; account keeps the ID. */
  accountName?: string;
  stream: string;
  consumer: string;
  server: string;
}

export interface Range {
  start: number;
  end: number;
}

export interface Window {
  start: number;
  end: number;
  label: string;
  detail: string;
}

export interface ConfigItem {
  key: string;
  value: string;
  highlight: boolean;
}

export interface Disagreement {
  field: string;
  values: { server: string; value: string }[];
}

export type FindingState = "firing" | "changed" | "flapping" | "resolved" | "unknown";

export interface Mute {
  id: string;
  checkCode: string;
  entity?: Entity;
  reason: string;
  mutedBy: string;
  createdAt?: number;
  expiresAt?: number;
  unmutedAt?: number;
  unmutedBy: string;
  /** An acceptance as by design: expiresAt is its review date. */
  accepted: boolean;
  /** The accepted entity's week's growth, bytes per day; undefined until nats-lens can measure it. */
  growthPerDay?: number;
  /** Why nats-lens ended the acceptance early; empty otherwise. */
  endReason: string;
}

export interface Finding {
  id: string;
  checkCode: string;
  checkName: string;
  severity: SeverityLevel;
  state: FindingState;
  entity?: Entity;
  /** When nats-lens first saw the finding. */
  since?: number;
  /** When the condition began, where the check knows it. */
  conditionSince?: number;
  transitionedAt?: number;
  resolvedAt?: number;
  detail: string;
  value?: { name: string; display: string; number?: number; basis: string };
  remediation: string;
  relatedConfig: ConfigItem[];
  relatedConfigSource: string;
  cause?: { id: string; time?: number; kind: string; summary: string };
  mute?: Mute;
  transitionsLastHour: number;
}

export interface Transition {
  time?: number;
  state: FindingState;
  severity: SeverityLevel;
  detail: string;
}

export interface Tally {
  critical: number;
  warning: number;
  info: number;
  muted: number;
  flapping: number;
  resolved24h: number;
}

export type Unit = "count" | "bytes" | "per_second" | "percent" | "seconds" | "bytes_per_day" | "bytes_per_second" | "unknown";

export interface Point {
  time: number;
  value: number;
}

export interface Series {
  metric: string;
  unit: Unit;
  stepMs: number;
  points: Point[];
  min?: number;
  max?: number;
}

export interface Tile {
  id: string;
  label: string;
  value?: number;
  unit: Unit;
  valueSuffix: string;
  value2?: number;
  value2Suffix: string;
  subtitle: string;
  status: Level;
  series?: Series;
  series2?: Series;
  limit?: number;
  // One sentence for the tile's tooltip.
  description?: string;
  // The value as text, for a tile whose value is a name.
  valueText?: string;
}

export type ServerRole = "meta leader" | "follower" | "no JetStream" | "unknown";

export interface Server {
  name: string;
  serverId: string;
  version: string;
  cluster: string;
  startTime?: number;
  uptimeMs?: number;
  role: ServerRole;
  present: boolean;
  healthz: string;
  healthzError: string;
  restarts: number[];
  connections?: number;
  jsStorageBytes?: number;
  jsMaxStorageBytes?: number;
  memBytes?: number;
  cpuPercent?: number;
  status: Level;
}

export interface Freshness {
  epoch: string;
  epochAt?: number;
  ageMs?: number;
  partial: boolean;
  live: boolean;
  intervalMs: number;
  /** Whether the system's collector collects; false while its connection to NATS is down. */
  collecting: boolean;
  /** When the collector stopped collecting; unset while it collects. */
  notCollectingSince?: number;
}

/** What a system's collector is doing. */
export type Collection = "unknown" | "starting" | "collecting" | "not connected" | "connection closed" | "demo";

/** One system as the Fleet page shows it; read from collected data only. */
export interface SystemSummary {
  id: string;
  name: string;
  environment: string;
  collection: Collection;
  /** Newest connection error while not collecting; empty while collecting. */
  collectionError: string;
  collectionErrorSince?: number;
  freshness?: Freshness;
  status: Level;
  worst?: Finding;
  tally: Tally;
  servers?: number;
  serversAnswering?: number;
  clusters?: number;
  streams?: number;
  connections?: number;
  /** Worst status per bucket, oldest first. */
  strip: Level[];
  stripStart?: number;
  stripBucketMs?: number;
  /** Why the summary could not be read; the other fields are then unset. */
  error: string;
}

/** One system's state for the sidebar's Fleet badge. */
export interface SystemBadge {
  id: string;
  name: string;
  environment: string;
  status: Level;
  collection: Collection;
  /** Whether the system's collector collects; false while its connection fails. */
  collecting: boolean;
  /** Why the status could not be read; the other fields are then unset. */
  error: string;
  /** Still being read; not counted until a later call has it. */
  pending: boolean;
}

/** What the shell of every page shows: the page's system and every system's badge. */
export interface ShellStatus {
  freshness?: Freshness;
  servers: Server[];
  tally: Tally;
  systems: SystemBadge[];
  /** The system a request without ?system= reads. */
  defaultSystem: string;
  /** Whether the system asked for is configured; false leaves freshness unset. */
  systemKnown: boolean;
  /** The failure of the system's own part, with the systems still listed. */
  error?: { code: string; message: string };
}

export interface Overview {
  cluster: string;
  range?: Range;
  freshness?: Freshness;
  servers: Server[];
  tally: Tally;
  worst?: Finding;
  tiles: Tile[];
  windows: Window[];
}

export type EventSourceName = "nats" | "kubernetes" | "finding" | "collector" | "other";

export interface LaneEvent {
  id: string;
  time: number;
  source: EventSourceName;
  kind: string;
  shortLabel: string;
  label: string;
  severity?: SeverityLevel;
  eventId?: string;
}

export interface TimelineRow {
  key: string;
  checkCode: string;
  checkName: string;
  severity: SeverityLevel;
  entity?: Entity;
  cells: Level[];
  now: Level;
}

export interface Timeline {
  start: number;
  bucketMs: number;
  bucketCount: number;
  rows: TimelineRow[];
  kubernetes: LaneEvent[];
  nats: LaneEvent[];
  windows: Window[];
  /** The range held more NATS lane events than the API returns. */
  natsTruncated: boolean;
  /** The range held more finding transitions than the API returns. */
  findingTruncated: boolean;
  /** The range held more Kubernetes lane events than the API returns. */
  kubernetesTruncated: boolean;
}

export interface LensEvent {
  id: string;
  time?: number;
  receivedAt?: number;
  source: EventSourceName;
  kind: string;
  type: string;
  severity?: SeverityLevel;
  summary: string;
  server: string;
  account: string;
  accountName?: string;
  stream: string;
  consumer: string;
  clientName: string;
  clientHost: string;
  reason: string;
  transition?: Transition;
  finding?: Finding;
  raised: Finding[];
  raw: string;
}

export interface Page<T> {
  items: T[];
  nextPageToken: string;
  totalCount: number;
}

export interface StreamSummary {
  entity: Entity;
  replicas?: number;
  messages?: number;
  bytes?: number;
  /** Consumers present; unset for a stream no server reports. */
  consumers?: number;
  consumersExpected?: number;
  bytesPerDay?: number;
  bytes7d?: Series;
  status: Level;
  findingCodes: string[];
  serversDisagree: boolean;
  leader: string;
}

export interface StreamTotals {
  bytes: number;
  durableConsumers: number;
  ephemeralConsumers: number;
}

export interface StreamServerView {
  server: string;
  isLeader: boolean;
  current: boolean;
  offline: boolean;
  lag?: number;
  messages?: number;
  bytes?: number;
  firstSeq?: number;
  lastSeq?: number;
  created?: number;
  configHash: string;
}

export interface ConsumerSummary {
  entity: Entity;
  durable: boolean;
  replicas?: number;
  numPending?: number;
  numAckPending?: number;
  numRedelivered?: number;
  numWaiting?: number;
  leader: string;
  lastDelivered?: number;
  paused: boolean;
  status: Level;
  findingCodes: string[];
  serversDisagree: boolean;
  present: boolean;
}

export interface ConsumerServerView {
  server: string;
  isLeader: boolean;
  current: boolean;
  offline: boolean;
  lag?: number;
  deliveredStreamSeq?: number;
  ackFloorStreamSeq?: number;
  created?: number;
  configHash: string;
}

export interface StreamDetail {
  stream?: StreamSummary;
  config: ConfigItem[];
  servers: StreamServerView[];
  disagreements: Disagreement[];
  consumers: ConsumerSummary[];
  findings: Finding[];
}

export interface ConsumerDetail {
  consumer?: ConsumerSummary;
  config: ConfigItem[];
  servers: ConsumerServerView[];
  disagreements: Disagreement[];
  findings: Finding[];
}

export interface ServerDetail {
  server?: Server;
  endpoints: { endpoint: string; state: string; error: string }[];
  processes: { serverId: string; startTime?: number; firstSeen?: number; lastSeen?: number }[];
  subscriptions?: number;
  slowConsumers?: number;
  routes?: number;
  jsStreams?: number;
  jsConsumers?: number;
  metaLeader: string;
  configDigest: string;
}

export interface Connection {
  key: string;
  server: string;
  cid: string;
  name: string;
  lang: string;
  version: string;
  account: string;
  accountName?: string;
  /** nats-lens's own connection or a loopback client, not an application. */
  tooling: boolean;
  user: string;
  ip: string;
  port?: number;
  startTime?: number;
  lastActivity?: number;
  rttMs?: number;
  pendingBytes?: number;
  inMsgs?: number;
  outMsgs?: number;
  inBytes?: number;
  outBytes?: number;
  stalls?: number;
  subscriptions?: number;
}

export interface ConnectionPage extends Page<Connection> {
  epoch: string;
  /** Tooling connections left out because they were not asked for. */
  toolingHidden: number;
  /** Every server seen in the range, sorted, whatever the filters and the page. */
  servers: string[];
}

/** How a write call names its user. */
export type IdentityMode = "iap" | "request" | "unknown";

export interface LensInfo {
  identityMode: IdentityMode;
  version: string;
  cluster: string;
  collectIntervalMs: number;
  retentionMs: number;
  live: boolean;
  demo: boolean;
  /** Start of the oldest stored epoch: the history begins here. */
  historyStart?: number;
}

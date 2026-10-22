// Subjects: GetSubjectsResponse and MatchSubjectResponse to the view models
// of the Subjects page. The only file that reads their messages; a value
// the server left unset stays undefined here, never 0.
import {
  SubjectFilter as PFilter,
  SubjectRowKind,
  type GetSubjectsResponse,
  type MatchSubjectResponse,
} from "../gen/natslens/v1/subjects_pb";
import { ms, n, optN } from "./adapt-common";

export type SubjectKind = "stream" | "kv" | "object" | "subscription" | "inbox" | "jsApi";

/** Which rows the page lists; "all" sends no filter. */
export type SubjectFilter = "all" | "stored" | "noConsumer" | "overlapping" | "wide";

export const FILTER_ENUM: Record<SubjectFilter, PFilter> = {
  all: PFilter.UNSPECIFIED,
  stored: PFilter.STORED,
  noConsumer: PFilter.NO_CONSUMER,
  overlapping: PFilter.OVERLAPPING,
  wide: PFilter.WIDE,
};

export interface SubjectRow {
  account: string;
  accountName: string;
  systemAccount: boolean;
  kind: SubjectKind;
  /** Empty for a mirror. */
  patterns: string[];
  /** Empty for a row that is not a stream's. */
  stream: string;
  mirrorOf: string;
  mirroredBy: string[];
  sourcedInto: string[];
  /** limits, interest or workqueue; empty when not reported or not a stream. */
  retention: string;
  messages?: number;
  durable: number;
  /** On a KV bucket, watchers. */
  ephemeral: number;
  connections: number;
  /** The queue group every subscription shares; empty for none. */
  queue: string;
  /** At most 3, distinct, in name order. */
  names: string[];
  moreNames: number;
  storedPerSecond?: number;
  apiPerSecond?: number;
  /** 0 to 1. */
  apiErrorShare?: number;
  overlaps: string[];
  noConsumer: boolean;
  wide: boolean;
}

export interface SubszServer {
  name: string;
  answered: boolean;
  total?: number;
  listed?: number;
}

export interface Subjects {
  /** False when the range holds no collection; everything else is then empty. */
  collected: boolean;
  epochAt?: number;
  /** Undefined: no subscription collection within 24 hours before the epoch. */
  subscriptionsAt?: number;
  rows: SubjectRow[];
  rowsTotal: number;
  counts: Record<SubjectFilter, number>;
  servers: SubszServer[];
  serverSubscriptions: number;
  unlistedSubscriptions: number;
}

const KIND: Record<SubjectRowKind, SubjectKind> = {
  [SubjectRowKind.UNSPECIFIED]: "subscription",
  [SubjectRowKind.STREAM]: "stream",
  [SubjectRowKind.KEY_VALUE]: "kv",
  [SubjectRowKind.OBJECT_STORE]: "object",
  [SubjectRowKind.SUBSCRIPTION]: "subscription",
  [SubjectRowKind.INBOX]: "inbox",
  [SubjectRowKind.JETSTREAM_API]: "jsApi",
};

export function subjects(r: GetSubjectsResponse): Subjects {
  return {
    collected: r.epoch !== 0n,
    epochAt: ms(r.epochAt),
    subscriptionsAt: ms(r.subscriptionsAt),
    rows: r.rows.map((s) => ({
      account: s.account,
      accountName: s.accountName,
      systemAccount: s.systemAccount,
      kind: KIND[s.kind] ?? "subscription",
      patterns: s.patterns,
      stream: s.stream,
      mirrorOf: s.mirrorOf,
      mirroredBy: s.mirroredBy,
      sourcedInto: s.sourcedInto,
      retention: s.retention,
      messages: optN(s.messages),
      durable: s.durableConsumers,
      ephemeral: s.ephemeralConsumers,
      connections: s.connections,
      queue: s.queue,
      names: s.clientNames,
      moreNames: s.moreNames,
      storedPerSecond: s.storedPerSecond,
      apiPerSecond: s.apiPerSecond,
      apiErrorShare: s.apiErrorShare,
      overlaps: s.overlaps,
      noConsumer: s.noConsumer,
      wide: s.wide,
    })),
    rowsTotal: r.rowsTotal,
    counts: { all: r.allCount, stored: r.storedCount, noConsumer: r.noConsumerCount, overlapping: r.overlappingCount, wide: r.wideCount },
    servers: r.subszServers.map((s) => ({ name: s.serverName, answered: s.answered, total: optN(s.total), listed: optN(s.listed) })),
    serverSubscriptions: n(r.serverSubscriptions),
    unlistedSubscriptions: n(r.unlistedConnectionSubscriptions),
  };
}

export interface StreamMatch {
  account: string;
  accountName: string;
  stream: string;
  pattern: string;
  /** Durable consumers whose filter matches, by name. */
  consumers: string[];
  ephemeral: number;
  mirrors: string[];
  sourcedBy: string[];
}

export interface CoreMatch {
  account: string;
  accountName: string;
  subject: string;
  queue: string;
  connections: number;
  names: string[];
  moreNames: number;
}

export interface SubjectMatches {
  collected: boolean;
  streams: StreamMatch[];
  core: CoreMatch[];
  /** Undefined: no subscription collection, and core is empty. */
  subscriptionsAt?: number;
}

export function subjectMatches(r: MatchSubjectResponse): SubjectMatches {
  return {
    collected: r.epoch !== 0n,
    streams: r.matches.map((m) => ({
      account: m.account,
      accountName: m.accountName,
      stream: m.stream,
      pattern: m.pattern,
      consumers: m.consumers,
      ephemeral: m.ephemeralConsumers,
      mirrors: m.mirrors,
      sourcedBy: m.sourcedBy,
    })),
    core: r.subscribers.map((c) => ({
      account: c.account,
      accountName: c.accountName,
      subject: c.subject,
      queue: c.queue,
      connections: c.connections,
      names: c.clientNames,
      moreNames: c.moreNames,
    })),
    subscriptionsAt: ms(r.subscriptionsAt),
  };
}

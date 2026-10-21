// Capacity: GetCapacityResponse to the view model of the Capacity page. The
// only file that reads its messages; a value the server left unset stays
// undefined here, never 0.
import { CapacityStatus, CapacityStreamKind, NextLimitKind, NextLimitState, type GetCapacityResponse } from "../gen/natslens/v1/capacity_pb";
import { dur, ms, optN, optSeverity } from "./adapt-common";
import { mute } from "./adapt-findings";
import type { Mute, SeverityLevel } from "./model";

export interface ServerStore {
  name: string;
  /** Empty when the server reports none. */
  cluster: string;
  /** False: the server's JSZ did not answer in the collection; used, limit and growth are unset. */
  reported: boolean;
  used?: number;
  /** Undefined when not reported, or reported as 0 (no limit reported). */
  limit?: number;
  /** Bytes per day over the range; undefined under 6 hours of data. */
  perDay?: number;
  /** Set only when perDay is above 0 and used and limit are known. */
  daysToFull?: number;
  /** server-014, else server-007; empty when neither fires. */
  findingId: string;
  findingCode: string;
  findingSeverity?: SeverityLevel;
  /** server-014's own days until full, by the check's rule. */
  findingDays?: number;
}

export type StreamKind = "stream" | "kv" | "object";
export type LimitKind = "none" | "empty" | "age" | "messages" | "bytes" | "perSubject" | "unknown";
export type LimitState = "steady" | "refusing" | "reaches" | "notReached" | "notForecast" | undefined;
export type Status = "fillsSoon" | "accepted" | "declared" | "muted" | "awaiting" | "unbounded" | "bounded" | "unknown";

export interface CapacityStream {
  account: string;
  accountName: string;
  stream: string;
  kind: StreamKind;
  /** limits, interest or workqueue; empty when not reported. */
  retention: string;
  /** file or memory; empty when not reported. */
  storage: string;
  replicas: number;
  mirror: string;
  sources: number;
  messages?: number;
  /** One copy. */
  bytes?: number;
  /** A KV bucket's keys. */
  keys?: number;
  firstTs?: number;
  maxMsgs?: number;
  maxBytes?: number;
  /** Milliseconds. */
  maxAge?: number;
  maxMsgsPerSubject?: number;
  /** old or new; empty when not reported. */
  discard: string;
  allowMsgTtl: boolean;
  /** One copy, bytes per day; undefined under 6 hours of data. */
  bytesPerDay?: number;
  msgsPerDay?: number;
  next: { limit: LimitKind; state: LimitState; days?: number; discardNew: boolean };
  status: Status;
  /** stream-006 for fills soon; stream-005 for accepted, muted and awaiting a decision. */
  findingId: string;
  /** stream-006's own days until max_bytes. */
  findingDays?: number;
  mute?: Mute;
  /** "inventory" or "writer_ttls" when declared. */
  declaredReason: string;
}

export interface Capacity {
  /** False when the range holds no collection; everything else is then empty. */
  collected: boolean;
  epochAt?: number;
  servers: ServerStore[];
  serversWithoutJetstream: number;
  streams: CapacityStream[];
  streamsTotal: number;
  /** The first collection the growth reads. */
  growthFrom?: number;
}

const KIND: Record<CapacityStreamKind, StreamKind> = {
  [CapacityStreamKind.UNSPECIFIED]: "stream",
  [CapacityStreamKind.STREAM]: "stream",
  [CapacityStreamKind.KEY_VALUE]: "kv",
  [CapacityStreamKind.OBJECT_STORE]: "object",
};

const LIMIT: Record<NextLimitKind, LimitKind> = {
  [NextLimitKind.UNSPECIFIED]: "unknown",
  [NextLimitKind.NONE]: "none",
  [NextLimitKind.EMPTY]: "empty",
  [NextLimitKind.AGE]: "age",
  [NextLimitKind.MESSAGES]: "messages",
  [NextLimitKind.BYTES]: "bytes",
  [NextLimitKind.PER_SUBJECT]: "perSubject",
};

const STATE: Record<NextLimitState, LimitState> = {
  [NextLimitState.UNSPECIFIED]: undefined,
  [NextLimitState.STEADY]: "steady",
  [NextLimitState.REACHED_REFUSING]: "refusing",
  [NextLimitState.REACHES]: "reaches",
  [NextLimitState.NOT_REACHED]: "notReached",
  [NextLimitState.NOT_FORECAST]: "notForecast",
};

const STATUS: Record<CapacityStatus, Status> = {
  [CapacityStatus.UNSPECIFIED]: "unknown",
  [CapacityStatus.FILLS_SOON]: "fillsSoon",
  [CapacityStatus.ACCEPTED]: "accepted",
  [CapacityStatus.DECLARED]: "declared",
  [CapacityStatus.MUTED]: "muted",
  [CapacityStatus.AWAITING_DECISION]: "awaiting",
  [CapacityStatus.UNBOUNDED]: "unbounded",
  [CapacityStatus.BOUNDED]: "bounded",
};

export function capacity(r: GetCapacityResponse): Capacity {
  return {
    collected: r.epoch !== 0n,
    epochAt: ms(r.epochAt),
    servers: r.servers.map((s) => ({
      name: s.serverName,
      cluster: s.cluster,
      reported: s.reported,
      used: optN(s.used),
      limit: optN(s.limit),
      perDay: s.perDay,
      daysToFull: s.daysToFull,
      findingId: s.findingId,
      findingCode: s.findingCode,
      findingSeverity: optSeverity(s.findingSeverity),
      findingDays: s.findingDays,
    })),
    serversWithoutJetstream: r.serversWithoutJetstream,
    streams: r.streams.map((s) => ({
      account: s.account,
      accountName: s.accountName,
      stream: s.stream,
      kind: KIND[s.kind] ?? "stream",
      retention: s.retention,
      storage: s.storage,
      replicas: s.replicas,
      mirror: s.mirror,
      sources: s.sources,
      messages: optN(s.messages),
      bytes: optN(s.bytes),
      keys: optN(s.keys),
      firstTs: ms(s.firstTs),
      maxMsgs: optN(s.maxMsgs),
      maxBytes: optN(s.maxBytes),
      maxAge: dur(s.maxAge),
      maxMsgsPerSubject: optN(s.maxMsgsPerSubject),
      discard: s.discard,
      allowMsgTtl: s.allowMsgTtl,
      bytesPerDay: s.bytesPerDay,
      msgsPerDay: s.msgsPerDay,
      next: {
        limit: LIMIT[s.nextLimit?.limit ?? NextLimitKind.UNSPECIFIED] ?? "unknown",
        state: STATE[s.nextLimit?.state ?? NextLimitState.UNSPECIFIED],
        days: s.nextLimit?.days,
        discardNew: s.nextLimit?.discardNew ?? false,
      },
      status: STATUS[s.status] ?? "unknown",
      findingId: s.findingId,
      findingDays: s.findingDays,
      mute: mute(s.mute),
      declaredReason: s.declaredReason,
    })),
    streamsTotal: r.streamsTotal,
    growthFrom: ms(r.growthFrom),
  };
}

// Micro services: GetMicroServicesResponse to the view model of the Micro
// services page. The only file that reads its messages; a value the
// server left unset stays undefined here, never 0.
import {
  MicroInstanceSource as PSource,
  MicroNote as PNote,
  MicroRequestState as PState,
  type GetMicroServicesResponse,
  type MicroRequest as PRequest,
} from "../gen/natslens/v1/micro_pb";
import { ms } from "./adapt-common";

/** The card's note: the first that applies, as the server chose it. */
export type MicroNoteKind = "missing" | "mixedVersions" | "slow" | "errors" | "noAnswer" | "notAsked" | "healthy";

/**
 * Where a service's instances come from: they answered the epoch's
 * STATS; none answered and the subscription list holds them; or the
 * account's STATS was not ok and no list holds them, so they are those
 * that last answered within the hour. "none": only missing ones.
 */
export type MicroInstanceSource = "none" | "answered" | "subscriptions" | "lastAnswered";

/**
 * How a $SRV request ended; "notAsked" when the account was not asked;
 * "held" when nats-lens did not send it because the server refused it
 * recently.
 */
export type MicroRequestState = "notAsked" | "ok" | "noResponders" | "timeout" | "error" | "held";

export interface MicroRequest {
  state: MicroRequestState;
  at?: number;
  replies: number;
  /** The client's or server's text; the server's refusal when held; "" otherwise. */
  error: string;
  /** Held only: when the server refused, and after when nats-lens asks again. */
  refusedAt?: number;
  againAfter?: number;
}

export interface MicroAccount {
  account: string;
  accountName: string;
  asked: boolean;
  info: MicroRequest;
  stats: MicroRequest;
  systemAccount: boolean;
  /** Services of the account in the list, before the limit. */
  services: number;
}

export interface MicroVersion {
  version: string;
  instances: number;
}

export interface MicroInstance {
  id: string;
  /** "" when not asked or not described yet. */
  version: string;
  /** Undefined when not asked. */
  started?: number;
  /** "" when unnamed or not found. */
  connectionName: string;
  server: string;
}

export interface MicroMissing {
  id: string;
  since?: number;
  lastSeen?: number;
  connectionName: string;
  /** The server of its connection when still subscribed; "" otherwise or when not found. */
  server: string;
  /** Registered (its $SRV subscriptions still listed) but not answering. */
  stillSubscribed: boolean;
}

export interface MicroEndpoint {
  name: string;
  subject: string;
  /** Every subject under this endpoint name, subject first; at least subject. */
  subjects: string[];
  /** "" for none. */
  queueGroup: string;
  requestsPerSecond?: number;
  averageMs?: number;
  /** 0 to 1. */
  errorShare?: number;
  lastError: string;
  lastErrorInstance: string;
  /** The 7-day level; undefined with under 24 hours of history. */
  levelMs?: number;
  slow: boolean;
  slowSince?: number;
  errorsHigh: boolean;
}

export interface MicroService {
  account: string;
  accountName: string;
  name: string;
  asked: boolean;
  /** Most instances first; empty when not asked. */
  versions: MicroVersion[];
  description: string;
  instances: number;
  missing: MicroMissing[];
  requestsPerSecond?: number;
  errorShare?: number;
  endpoints: MicroEndpoint[];
  endpointsTotal: number;
  connectionNames: string[];
  moreNames: number;
  note: MicroNoteKind;
  systemAccount: boolean;
  instanceList: MicroInstance[];
  instanceSource: MicroInstanceSource;
  answered: boolean;
}

export interface MicroServices {
  /** False when the range holds no collection; everything else is then empty. */
  collected: boolean;
  epochAt?: number;
  /** Undefined: no subscription collection within 24 hours before the epoch. */
  subscriptionsAt?: number;
  services: MicroService[];
  servicesTotal: number;
  accounts: MicroAccount[];
}

const NOTE: Record<PNote, MicroNoteKind> = {
  [PNote.UNSPECIFIED]: "healthy",
  [PNote.INSTANCE_MISSING]: "missing",
  [PNote.MIXED_VERSIONS]: "mixedVersions",
  [PNote.SLOW_ENDPOINT]: "slow",
  [PNote.ERRORS]: "errors",
  [PNote.NO_ANSWER]: "noAnswer",
  [PNote.NOT_ASKED]: "notAsked",
  [PNote.HEALTHY]: "healthy",
};

const SOURCE: Record<PSource, MicroInstanceSource> = {
  [PSource.UNSPECIFIED]: "none",
  [PSource.ANSWERED]: "answered",
  [PSource.SUBSCRIPTIONS]: "subscriptions",
  [PSource.LAST_ANSWERED]: "lastAnswered",
};

const STATE: Record<PState, MicroRequestState> = {
  [PState.UNSPECIFIED]: "notAsked",
  [PState.OK]: "ok",
  [PState.NO_RESPONDERS]: "noResponders",
  [PState.TIMEOUT]: "timeout",
  [PState.ERROR]: "error",
  [PState.HELD]: "held",
};

function request(r: PRequest | undefined): MicroRequest {
  if (!r) return { state: "notAsked", replies: 0, error: "" };
  return { state: STATE[r.state] ?? "notAsked", at: ms(r.at), replies: r.replies, error: r.error, refusedAt: ms(r.refusedAt), againAfter: ms(r.retryAfter) };
}

export function microServices(r: GetMicroServicesResponse): MicroServices {
  return {
    collected: r.epoch !== 0n,
    epochAt: ms(r.epochAt),
    subscriptionsAt: ms(r.subscriptionsAt),
    services: r.services.map((s) => ({
      account: s.account,
      accountName: s.accountName,
      name: s.name,
      asked: s.asked,
      versions: s.versions.map((v) => ({ version: v.version, instances: v.instances })),
      description: s.description,
      instances: s.instances,
      missing: s.missing.map((m) => ({
        id: m.id,
        since: ms(m.since),
        lastSeen: ms(m.lastSeen),
        connectionName: m.connectionName,
        server: m.server,
        stillSubscribed: m.stillSubscribed,
      })),
      requestsPerSecond: s.requestsPerSecond,
      errorShare: s.errorShare,
      endpoints: s.endpoints.map((e) => ({
        name: e.name,
        subject: e.subject,
        subjects: e.subjects.length ? e.subjects : e.subject ? [e.subject] : [],
        queueGroup: e.queueGroup,
        requestsPerSecond: e.requestsPerSecond,
        averageMs: e.averageMs,
        errorShare: e.errorShare,
        lastError: e.lastError,
        lastErrorInstance: e.lastErrorInstance,
        levelMs: e.levelMs,
        slow: e.slow,
        slowSince: ms(e.slowSince),
        errorsHigh: e.errorsHigh,
      })),
      endpointsTotal: s.endpointsTotal,
      connectionNames: s.connectionNames,
      moreNames: s.moreNames,
      note: NOTE[s.note] ?? "healthy",
      systemAccount: s.systemAccount,
      instanceList: s.instanceList.map((i) => ({
        id: i.id,
        version: i.version,
        started: ms(i.started),
        connectionName: i.connectionName,
        server: i.server,
      })),
      instanceSource: SOURCE[s.instanceSource] ?? "none",
      answered: s.answered,
    })),
    servicesTotal: r.servicesTotal,
    accounts: r.accounts.map((a) => ({
      account: a.account,
      accountName: a.accountName,
      asked: a.asked,
      info: request(a.info),
      stats: request(a.stats),
      systemAccount: a.systemAccount,
      services: a.services,
    })),
  };
}

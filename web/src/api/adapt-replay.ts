// Proto to view model: the Replay page's story between two moments.
import {
  ReplayEvidenceKind as PEvidenceKind,
  ReplayGapKind as PGapKind,
  ReplayUnobservedReason as PReason,
  ReplayStepKind as PStepKind,
  type GetReplayResponse,
  type GoneAsset as PGone,
  type ReplayCounts as PCounts,
  type ReplayStep as PStep,
} from "../gen/natslens/v1/replay_pb";
import { entity, entityOrEmpty, ms, optSeverity } from "./adapt-common";
import type { Entity, SeverityLevel } from "./model";

export type ReplayStepKind = "kubernetes" | "restart" | "leaders" | "quorum" | "gone" | "finding" | "resolved" | "gap" | "other";

export type ReplayEvidenceKind = "event" | "finding" | "collection" | "other";

/** Why a count was not observed, as GetReplay says it. */
export type UnobservedReason =
  | "no_collections"
  | "no_collection_at_a"
  | "no_collection_at_b"
  | "collections_gap"
  | "events_not_captured"
  | "collector_gap"
  | "read_limit"
  | "kubernetes_not_watched"
  | "kubernetes_gap"
  | "node_unnamed"
  | "no_pool_labels"
  | "gone_not_read";

const REASON: Partial<Record<PReason, UnobservedReason>> = {
  [PReason.NO_COLLECTIONS]: "no_collections",
  [PReason.NO_COLLECTION_AT_A]: "no_collection_at_a",
  [PReason.NO_COLLECTION_AT_B]: "no_collection_at_b",
  [PReason.COLLECTIONS_GAP]: "collections_gap",
  [PReason.EVENTS_NOT_CAPTURED]: "events_not_captured",
  [PReason.COLLECTOR_GAP]: "collector_gap",
  [PReason.READ_LIMIT]: "read_limit",
  [PReason.KUBERNETES_NOT_WATCHED]: "kubernetes_not_watched",
  [PReason.KUBERNETES_GAP]: "kubernetes_gap",
  [PReason.NODE_UNNAMED]: "node_unnamed",
  [PReason.NO_POOL_LABELS]: "no_pool_labels",
  [PReason.GONE_NOT_READ]: "gone_not_read",
};

/** Why one count is unset; reason unset when the API gives none this UI knows. */
export interface Unobserved {
  /** The ReplayCounts field, as the view model names it: "leadersMoved". */
  count: string;
  reason?: UnobservedReason;
  from?: number;
  to?: number;
}

/** "leaders_moved" to "leadersMoved". */
function camel(name: string): string {
  return name.replace(/_([a-z])/g, (_, c: string) => c.toUpperCase());
}

/** What a gap step's time lacked: collections, nats-lens running, or the Kubernetes watch. */
export type ReplayGapKind = "collections" | "collector" | "kubernetes";

const GAP_KIND: Partial<Record<PGapKind, ReplayGapKind>> = {
  [PGapKind.COLLECTIONS]: "collections",
  [PGapKind.COLLECTOR]: "collector",
  [PGapKind.KUBERNETES]: "kubernetes",
};

const STEP_KIND: Record<PStepKind, ReplayStepKind> = {
  [PStepKind.UNSPECIFIED]: "other",
  [PStepKind.KUBERNETES]: "kubernetes",
  [PStepKind.RESTART]: "restart",
  [PStepKind.LEADERS]: "leaders",
  [PStepKind.QUORUM]: "quorum",
  [PStepKind.GONE]: "gone",
  [PStepKind.FINDING]: "finding",
  [PStepKind.RESOLVED]: "resolved",
  [PStepKind.GAP]: "gap",
};

const EVIDENCE_KIND: Record<PEvidenceKind, ReplayEvidenceKind> = {
  [PEvidenceKind.UNSPECIFIED]: "other",
  [PEvidenceKind.EVENT]: "event",
  [PEvidenceKind.FINDING]: "finding",
  [PEvidenceKind.COLLECTION]: "collection",
};

export interface ReplayEvidence {
  kind: ReplayEvidenceKind;
  /** Event ID, finding ID, or epoch number. */
  id: string;
  time?: number;
  label: string;
  /** For a collection, the entity to open at that moment; unset is the system. */
  entity?: Entity;
}

export interface ReplayStep {
  id: string;
  time: number;
  /** When the span ended; unset for a moment and for a span still open at B. */
  end?: number;
  openAtB: boolean;
  kind: ReplayStepKind;
  /** Unset is neutral. */
  severity?: SeverityLevel;
  title: string;
  body: string;
  evidence: ReplayEvidence[];
  evidenceCount: number;
  /** After B: it closes a step in the range. */
  outsideRange: boolean;
  /** For a gap step, what its time lacked; unset when the API does not say. */
  gapKind?: ReplayGapKind;
}

/** An unset count was not observed; zero means none was observed. */
export interface ReplayCounts {
  serversRestarted?: number;
  serversStopped?: number;
  streamsGoneWithoutDelete?: number;
  consumersGoneWithoutDelete?: number;
  assetsGoneWithDelete?: number;
  assetsUnknownAtB?: number;
  leadersMoved?: number;
  leaderElections?: number;
  metaLeaderChanges?: number;
  quorumLostReports?: number;
  nodePoolsUpgraded?: number;
  nodesUpgraded?: number;
  nodesDrained?: number;
  clientReconnects?: number;
  findingsRaised?: number;
}

export interface GoneAsset {
  entity: Entity;
  serversAtA: string[];
  notAnsweringAtB: string[];
  lastReported?: number;
  deleteEvent?: { id: string; time?: number; kind: string; summary: string };
  /** "analytics-worker (10.20.0.12)"; empty without one. */
  deleteClient: string;
  consumersWithIt: number;
}

export interface Replay {
  /** What the request asked for, so a page can tell its own answer from an earlier one. */
  asked: { system: string; a: number; b: number };
  a: number;
  /** B after the server read a B after now as now. */
  b: number;
  epochA: string;
  /** Unset: no collection at A. */
  epochAAt?: number;
  epochB: string;
  epochBAt?: number;
  /** In time order; steps outside the range last. */
  steps: ReplayStep[];
  stepsTruncated: boolean;
  stepsOmitted: number;
  counts: ReplayCounts;
  gone: GoneAsset[];
  goneTruncated: boolean;
  kubernetesWatched: boolean;
  /** When the stored history begins. */
  historyStart?: number;
  /** The read limit cut events or transitions: steps after it may be missing. */
  completeUntil?: number;
  /** Why each unset count is unset, one per count. */
  unobserved: Unobserved[];
}

function step(s: PStep): ReplayStep {
  return {
    id: s.id,
    time: ms(s.time) ?? 0,
    end: ms(s.end),
    openAtB: s.openAtB,
    kind: STEP_KIND[s.kind] ?? "other",
    severity: optSeverity(s.severity),
    title: s.title,
    body: s.body,
    evidence: s.evidence.map((e) => ({ kind: EVIDENCE_KIND[e.kind] ?? "other", id: e.id, time: ms(e.time), label: e.label, entity: entity(e.entity) })),
    evidenceCount: s.evidenceCount,
    outsideRange: s.outsideRange,
    gapKind: GAP_KIND[s.gapKind],
  };
}

function counts(c: PCounts | undefined): ReplayCounts {
  if (!c) return {};
  return {
    serversRestarted: c.serversRestarted,
    serversStopped: c.serversStopped,
    streamsGoneWithoutDelete: c.streamsGoneWithoutDelete,
    consumersGoneWithoutDelete: c.consumersGoneWithoutDelete,
    assetsGoneWithDelete: c.assetsGoneWithDelete,
    assetsUnknownAtB: c.assetsUnknownAtB,
    leadersMoved: c.leadersMoved,
    leaderElections: c.leaderElections,
    metaLeaderChanges: c.metaLeaderChanges,
    quorumLostReports: c.quorumLostReports,
    nodePoolsUpgraded: c.nodePoolsUpgraded,
    nodesUpgraded: c.nodesUpgraded,
    nodesDrained: c.nodesDrained,
    clientReconnects: c.clientReconnects,
    findingsRaised: c.findingsRaised,
  };
}

function gone(g: PGone): GoneAsset {
  const d = g.deleteEvent;
  return {
    entity: entityOrEmpty(g.entity),
    serversAtA: g.serversAtA,
    notAnsweringAtB: g.notAnsweringAtB,
    lastReported: ms(g.lastReported),
    deleteEvent: d && d.id ? { id: d.id, time: ms(d.time), kind: d.kind, summary: d.summary } : undefined,
    deleteClient: g.deleteClient,
    consumersWithIt: g.consumersWithIt,
  };
}

export function replay(r: GetReplayResponse, asked: Replay["asked"]): Replay {
  return {
    asked,
    a: ms(r.a) ?? asked.a,
    b: ms(r.b) ?? asked.b,
    epochA: String(r.epochA),
    epochAAt: ms(r.epochAAt),
    epochB: String(r.epochB),
    epochBAt: ms(r.epochBAt),
    steps: r.steps.map(step),
    stepsTruncated: r.stepsTruncated,
    stepsOmitted: r.stepsOmitted,
    counts: counts(r.counts),
    gone: r.gone.map(gone),
    goneTruncated: r.goneTruncated,
    kubernetesWatched: r.kubernetesWatched,
    historyStart: ms(r.historyStart),
    completeUntil: ms(r.completeUntil),
    unobserved: r.unobserved.map((u) => ({ count: camel(u.count), reason: REASON[u.reason], from: ms(u.from), to: ms(u.to) })),
  };
}

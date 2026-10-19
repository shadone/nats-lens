// Proto to view model: the Replay page's story between two moments.
import {
  ReplayEvidenceKind as PEvidenceKind,
  ReplayStepKind as PStepKind,
  type GetReplayResponse,
  type GoneAsset as PGone,
  type ReplayCounts as PCounts,
  type ReplayStep as PStep,
} from "../gen/natslens/v1/replay_pb";
import { entity, entityOrEmpty, ms, optSeverity, window } from "./adapt-common";
import type { Entity, SeverityLevel, Window } from "./model";

export type ReplayStepKind = "kubernetes" | "restart" | "leaders" | "quorum" | "gone" | "finding" | "resolved" | "gap" | "other";

export type ReplayEvidenceKind = "event" | "finding" | "collection" | "other";

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
  entities: Entity[];
  entityCount: number;
  evidence: ReplayEvidence[];
  evidenceCount: number;
  /** After B: it closes a step in the range. */
  outsideRange: boolean;
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
  lastEpoch: string;
  deleteEvent?: { id: string; time?: number; kind: string; summary: string };
  /** "analytics-worker (10.20.0.12)"; empty without one. */
  deleteClient: string;
  consumersWithIt: number;
  findingIds: string[];
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
  windows: Window[];
  kubernetesWatched: boolean;
  historyStart?: number;
  /** The read limit cut events or transitions: steps after it may be missing. */
  completeUntil?: number;
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
    entities: s.entities.map(entityOrEmpty),
    entityCount: s.entityCount,
    evidence: s.evidence.map((e) => ({ kind: EVIDENCE_KIND[e.kind] ?? "other", id: e.id, time: ms(e.time), label: e.label, entity: entity(e.entity) })),
    evidenceCount: s.evidenceCount,
    outsideRange: s.outsideRange,
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
    lastEpoch: String(g.lastEpoch),
    deleteEvent: d && d.id ? { id: d.id, time: ms(d.time), kind: d.kind, summary: d.summary } : undefined,
    deleteClient: g.deleteClient,
    consumersWithIt: g.consumersWithIt,
    findingIds: g.findingIds,
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
    windows: r.windows.map(window),
    kubernetesWatched: r.kubernetesWatched,
    historyStart: ms(r.historyStart),
    completeUntil: ms(r.completeUntil),
  };
}

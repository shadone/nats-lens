// The Replay page's rules shared by the page and the incident log: how a
// step's time reads, where its evidence links to, and the count tiles.
import type { GoneAsset, Replay, ReplayCounts, ReplayEvidence, ReplayStep, ReplayStepKind } from "../api/adapt-replay";
import type { Entity } from "../api/model";
import { dayMon, dayMonHm, hm, hms } from "./format";
import { momentIso, type Moments } from "./replayUrl";
import { entityPath, eventPath, findingPath } from "./view";

const HOUR = 3_600_000;

export const KIND_LABEL: Record<ReplayStepKind, string> = {
  kubernetes: "Kubernetes",
  restart: "Restart",
  leaders: "Leaders",
  quorum: "Quorum",
  gone: "Gone",
  finding: "Finding",
  resolved: "Resolved",
  gap: "Gap",
  other: "Step",
};

export const FOOTER = "Built from collections, NATS advisories and Kubernetes events; every line links to its evidence.";

/** A time of a step: 09:14:05, with the day when A and B are on different days. */
export function whenFormat(m: Moments): (t: number) => string {
  return dayMon(m.a) === dayMon(m.b) ? hms : (t) => `${dayMon(t)} ${hms(t)}`;
}

/** "09:14:05", "09:14:05–09:16:30", or "09:14:05–still at B". */
export function stepWhen(s: ReplayStep, fmt: (t: number) => string): string {
  if (s.end !== undefined && s.end !== s.time) return `${fmt(s.time)}–${fmt(s.end)}`;
  if (s.openAtB) return `${fmt(s.time)}–still at B`;
  return fmt(s.time);
}

/** The dot's class: the Kubernetes colour, the OK colour for a resolve, a dashed gap, else the severity. */
export function dotClass(s: ReplayStep): string {
  if (s.kind === "kubernetes") return "k8s";
  if (s.kind === "resolved") return "ok";
  if (s.kind === "gap") return "gap";
  return s.severity ?? "neutral";
}

/** A fixed range of the hour before t, ending a second after it, so a page shows the state at t. */
export function rangeEndingAt(t: number): string {
  return `from=${momentIso(t - HOUR)}&to=${momentIso(t + 1000)}`;
}

/**
 * Where a piece of evidence opens: an event on the Events page, a finding
 * on its page, a collection on its entity's page (or Placement for the
 * system) over a range ending just after it.
 */
export function evidencePath(e: ReplayEvidence): string | undefined {
  switch (e.kind) {
    case "event":
      return eventPath(e.id);
    case "finding":
      return findingPath(e.id);
    case "collection": {
      if (e.time === undefined) return undefined;
      return `${collectionPage(e.entity)}?${rangeEndingAt(e.time)}`;
    }
    default:
      return undefined;
  }
}

function collectionPage(e: Entity | undefined): string {
  const p = e && (e.kind === "server" || e.kind === "stream" || e.kind === "consumer") ? entityPath(e) : undefined;
  return p ?? "/placement";
}

/** "event", "finding", "collection": what a link opens. */
export function evidenceWord(e: ReplayEvidence): string {
  return e.kind === "other" ? "record" : e.kind;
}

/** A gone asset's page as it was at A. */
export function gonePath(g: GoneAsset, a: number): string | undefined {
  const p = entityPath(g.entity);
  return p ? `${p}?${rangeEndingAt(a)}` : undefined;
}

/** Placement at B, compared with A. */
export function comparePath(m: Moments): string {
  return `/placement?from=${momentIso(m.b - HOUR)}&to=${momentIso(m.b)}&cmp=${momentIso(m.a)}`;
}

/** "stream", "KV bucket", "object store", "consumer". */
export function goneKind(g: GoneAsset): string {
  if (g.entity.kind === "consumer") return "consumer";
  if (g.entity.stream.startsWith("KV_")) return "KV bucket";
  if (g.entity.stream.startsWith("OBJ_")) return "object store";
  return "stream";
}

/** "none", or "none answering: nats-us-4" when holders did not answer at B. */
export function goneAtB(g: GoneAsset): string {
  return g.notAnsweringAtB.length ? `none answering: ${g.notAnsweringAtB.join(", ")}` : "none";
}

/** "3 servers: nats-0, nats-1, nats-2" */
export function serversText(names: string[]): string {
  if (!names.length) return "none";
  return `${names.length} ${names.length === 1 ? "server" : "servers"}: ${names.join(", ")}`;
}

export interface CountTile {
  label: string;
  /** Unset: not observed. */
  value?: number;
  note?: string;
  /** The colour of a count above zero, as the board draws it. */
  tone?: "warning" | "critical" | "k8s" | "muted";
  /** Counted from the Kubernetes watch. */
  k8s?: boolean;
}

/** The tiles of "Between A and B", in order; an unset count stays unset. */
export function countTiles(c: ReplayCounts): CountTile[] {
  const pools: CountTile =
    c.nodePoolsUpgraded === undefined && c.nodesUpgraded !== undefined
      ? { label: "Nodes upgraded", value: c.nodesUpgraded, note: "no node pool labels", tone: "k8s", k8s: true }
      : {
          label: "Node pools upgraded",
          value: c.nodePoolsUpgraded,
          note: c.nodesUpgraded !== undefined ? `${c.nodesUpgraded} ${c.nodesUpgraded === 1 ? "node" : "nodes"}` : undefined,
          tone: "k8s",
          k8s: true,
        };
  return [
    { label: "Servers restarted", value: c.serversRestarted, tone: "warning" },
    { label: "Servers stopped", value: c.serversStopped, note: "not back by B", tone: "warning" },
    { label: "Streams gone without a delete", value: c.streamsGoneWithoutDelete, tone: "critical" },
    { label: "Consumers gone without a delete", value: c.consumersGoneWithoutDelete, tone: "critical" },
    { label: "Assets gone with a delete", value: c.assetsGoneWithDelete },
    { label: "Assets unknown at B", value: c.assetsUnknownAtB, note: "a holder did not answer at B", tone: "warning" },
    {
      label: "Leaders moved",
      value: c.leadersMoved,
      note: c.leaderElections !== undefined ? `of ${c.leaderElections} ${c.leaderElections === 1 ? "election" : "elections"}` : undefined,
    },
    { label: "Meta leader changes", value: c.metaLeaderChanges },
    { label: "Quorum lost advisories", value: c.quorumLostReports, note: "NATS advisories, not server logs", tone: "critical" },
    pools,
    { label: "Nodes drained", value: c.nodesDrained, tone: "k8s", k8s: true },
    { label: "Client reconnects", value: c.clientReconnects, tone: "muted" },
    { label: "Findings raised", value: c.findingsRaised, note: "warning and critical", tone: "warning" },
  ];
}

/** What may be missing from the steps, one sentence each. */
export function stepNotes(r: Pick<Replay, "a" | "b" | "stepsTruncated" | "stepsOmitted" | "completeUntil">): string[] {
  const out: string[] = [];
  if (r.completeUntil !== undefined) {
    const at = dayMon(r.a) === dayMon(r.b) ? hm(r.completeUntil) : dayMonHm(r.completeUntil);
    out.push(`Steps after ${at} may be missing: the read limit was reached.`);
  }
  if (r.stepsOmitted > 0) {
    out.push(
      `${r.stepsOmitted} ${r.stepsOmitted === 1 ? "step was" : "steps were"} left out to keep at most 200 between A and B and 10 after B; critical, warning, restart, gone and gap steps are kept first.`,
    );
  }
  if (r.stepsTruncated && !out.length) out.push("Some steps may be missing.");
  return out;
}

/** What the page says when there was no collection at A or at B. */
export function collectionNotes(r: Pick<Replay, "a" | "epochAAt" | "epochBAt" | "historyStart">): string[] {
  const out: string[] = [];
  if (r.historyStart !== undefined && r.a < r.historyStart) {
    out.push(`A is before the history starts, ${dayMonHm(r.historyStart)} UTC: nats-lens stored nothing before then.`);
  }
  const why = "none started in the three collect intervals before it, so the counts that need it are not observed.";
  if (r.epochAAt === undefined) out.push(`No collection at A: ${why}`);
  if (r.epochBAt === undefined) out.push(`No collection at B: ${why}`);
  return out;
}

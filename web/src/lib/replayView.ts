// The Replay page's rules shared by the page and the incident log: how a
// step's time reads, where its evidence links to, and the count tiles.
import type { GoneAsset, Replay, ReplayCounts, Unobserved, ReplayEvidence, ReplayStep, ReplayStepKind } from "../api/adapt-replay";
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
  // A time on another day than A's carries the day, so a gap from the
  // evening before never reads as one in the same night.
  if (dayMon(m.a) !== dayMon(m.b)) return (t) => `${dayMon(t)} ${hms(t)}`;
  return (t) => (sameDay(t, m.a) ? hms(t) : `${dayMon(t)} ${hms(t)}`);
}

/** Whether two times fall on the same UTC day. */
function sameDay(x: number, y: number): boolean {
  return Math.floor(x / 86_400_000) === Math.floor(y / 86_400_000);
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

export type EvidenceItem = { kind: "one"; e: ReplayEvidence } | { kind: "elections"; leader: string; count: number; items: ReplayEvidence[] };

/**
 * A step's evidence as the page lists it, and how many records are left
 * out beyond it. The leader elections (rows that name the server they
 * elected) fold into one item per new leader, in the place of its first
 * row, when the step holds two or more elections on that leader; the
 * count comes from the step's leaders taken, which the evidence cap does
 * not cut, and a leader whose elections all fell past the cap gets an item
 * without rows. "More" then counts only the other records left out. A meta
 * leader step is not folded: its few rows keep their time order.
 */
export function groupEvidence(s: Pick<ReplayStep, "evidence" | "evidenceCount" | "leadersTaken" | "meta">): { items: EvidenceItem[]; more: number } {
  const { evidence, leadersTaken } = s;
  if (s.meta) return { items: evidence.map((e) => ({ kind: "one", e })), more: Math.max(0, s.evidenceCount - evidence.length) };
  const shown = new Map<string, number>();
  for (const e of evidence) if (e.elected) shown.set(e.elected, (shown.get(e.elected) ?? 0) + 1);
  const total = (l: string) => Math.max(leadersTaken.find((x) => x.leader === l)?.count ?? 0, shown.get(l) ?? 0);
  const items: EvidenceItem[] = [];
  const groups = new Map<string, { kind: "elections"; leader: string; count: number; items: ReplayEvidence[] }>();
  for (const e of evidence) {
    const l = e.elected;
    if (!l || total(l) < 2) {
      items.push({ kind: "one", e });
      continue;
    }
    let g = groups.get(l);
    if (!g) {
      g = { kind: "elections", leader: l, count: total(l), items: [] };
      groups.set(l, g);
      items.push(g);
    }
    g.items.push(e);
  }
  for (const l of leadersTaken) {
    if (l.count > 0 && !shown.has(l.leader)) items.push({ kind: "elections", leader: l.leader, count: l.count, items: [] });
  }
  // Elections not listed are told by their groups, not by "more".
  const electionsShown = [...shown.values()].reduce((a, b) => a + b, 0);
  const electionsAll = leadersTaken.reduce((a, l) => a + l.count, 0);
  const hiddenElections = Math.max(0, electionsAll - electionsShown);
  return { items, more: Math.max(0, s.evidenceCount - evidence.length - hiddenElections) };
}

/** "11 leader elections on nats-1 (6 of 11 listed)", "3 leader elections on nats-3, none listed" */
export function electionsText(g: { leader: string; count: number; items: ReplayEvidence[] }): string {
  const head = `${g.count} leader ${g.count === 1 ? "election" : "elections"} on ${g.leader}`;
  if (g.items.length === 0) return `${head}, none listed`;
  return g.items.length < g.count ? `${head} (${g.items.length} of ${g.count} listed)` : head;
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
  /** The ReplayCounts field the tile shows. */
  key: keyof ReplayCounts;
  label: string;
  /** Unset: not observed. */
  value?: number;
  note?: string;
  /** The colour of a count above zero, as the board draws it. */
  tone?: "warning" | "critical" | "k8s" | "muted";
}

/** The tiles of "Between A and B", in order; an unset count stays unset. */
export function countTiles(c: ReplayCounts): CountTile[] {
  const pools: CountTile =
    c.nodePoolsUpgraded === undefined && c.nodesUpgraded !== undefined
      ? { key: "nodesUpgraded", label: "Nodes upgraded", value: c.nodesUpgraded, note: "no node pool labels", tone: "k8s" }
      : {
          key: "nodePoolsUpgraded",
          label: "Node pools upgraded",
          value: c.nodePoolsUpgraded,
          note: c.nodesUpgraded !== undefined ? `${c.nodesUpgraded} ${c.nodesUpgraded === 1 ? "node" : "nodes"}` : undefined,
          tone: "k8s",
        };
  return [
    { key: "serversRestarted", label: "Servers restarted", value: c.serversRestarted, tone: "warning" },
    { key: "serversStopped", label: "Servers stopped", value: c.serversStopped, note: "not back by B", tone: "warning" },
    { key: "streamsGoneWithoutDelete", label: "Streams gone without a delete", value: c.streamsGoneWithoutDelete, tone: "critical" },
    { key: "consumersGoneWithoutDelete", label: "Consumers gone without a delete", value: c.consumersGoneWithoutDelete, tone: "critical" },
    { key: "assetsGoneWithDelete", label: "Assets gone with a delete", value: c.assetsGoneWithDelete },
    { key: "assetsUnknownAtB", label: "Assets unknown at B", value: c.assetsUnknownAtB, note: "a holder did not answer at B", tone: "warning" },
    {
      key: "leadersMoved",
      label: "Leaders moved",
      value: c.leadersMoved,
      note: c.leaderElections ? `of ${c.leaderElections} ${c.leaderElections === 1 ? "election" : "elections"}` : undefined,
    },
    { key: "metaLeaderChanges", label: "Meta leader changes", value: c.metaLeaderChanges },
    { key: "quorumLostReports", label: "Quorum lost advisories", value: c.quorumLostReports, note: "NATS advisories, not server logs", tone: "critical" },
    pools,
    { key: "nodesDrained", label: "Nodes drained", value: c.nodesDrained, tone: "k8s" },
    { key: "clientReconnects", label: "Client reconnects", value: c.clientReconnects, tone: "muted" },
    { key: "findingsRaised", label: "Findings raised", value: c.findingsRaised, note: "warning and critical", tone: "warning" },
  ];
}

/** "09:40–09:55", "09:40–still at B", or "" without times. */
function span(u: Unobserved, fmt: (t: number) => string): string {
  if (u.from === undefined) return "";
  return u.to !== undefined ? `${fmt(u.from)}–${fmt(u.to)}` : `${fmt(u.from)}–still at B`;
}

/** Why a count was not observed, in words with the times of the gap or cut; undefined for a reason this UI does not know. */
export function unobservedText(u: Unobserved, fmt: (t: number) => string): string | undefined {
  const at = (s: string) => (s ? ` ${s}` : "");
  switch (u.reason) {
    case "no_collections":
      return "no collection started between A and B";
    case "no_collection_at_a":
      return "no collection at A";
    case "no_collection_at_b":
      return "no collection at B";
    case "events_not_captured":
      return "no event was stored up to B";
    case "collector_gap":
      return `nats-lens was not running${at(span(u, fmt))}`;
    case "read_limit":
      return u.from !== undefined ? `the read limit was reached at ${fmt(u.from)}` : "the read limit was reached";
    case "kubernetes_not_watched":
      return "Kubernetes is not watched for this system";
    case "kubernetes_gap":
      return `Kubernetes was not watched${at(span(u, fmt))}`;
    case "node_unnamed":
      return "a node event names no node";
    case "no_pool_labels":
      return "no upgraded node carried a node pool label";
    case "events_connection_lost":
      return `no events captured: an events connection was lost${at(span(u, fmt))}`;
    case "gone_not_read":
      return "the gone assets were not read";
    default:
      return undefined;
  }
}

export interface UnsetGroup {
  /** Why these counts are unset; undefined when the response gives no reason this UI knows. */
  reason?: string;
  tiles: CountTile[];
}

/**
 * The unset tiles grouped by why, in tile order: one group per reason and
 * its times, as GetReplay's unobserved gives them, and one without a
 * reason for the rest.
 */
export function unsetGroups(tiles: CountTile[], unobserved: Unobserved[], fmt: (t: number) => string): UnsetGroup[] {
  const groups = new Map<string, UnsetGroup>();
  for (const t of tiles) {
    if (t.value !== undefined) continue;
    const u = unobserved.find((x) => x.count === t.key);
    const reason = u ? unobservedText(u, fmt) : undefined;
    const key = reason ?? "";
    const g = groups.get(key) ?? { reason, tiles: [] };
    g.tiles.push(t);
    groups.set(key, g);
  }
  return [...groups.values()];
}

/** What may be missing from the steps, one sentence each. */
export function stepNotes(r: Pick<Replay, "a" | "b" | "stepsTruncated" | "stepsOmitted" | "completeUntil">): string[] {
  const out: string[] = [];
  if (r.completeUntil !== undefined) {
    const at = sameDay(r.a, r.b) && sameDay(r.completeUntil, r.a) ? hm(r.completeUntil) : dayMonHm(r.completeUntil);
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

/** What the page says when A is before the history starts; undefined when it is not. */
export function beforeHistoryNote(r: Pick<Replay, "a" | "historyStart">): string | undefined {
  if (r.historyStart === undefined || r.a >= r.historyStart) return undefined;
  return `A is before the history starts, ${dayMonHm(r.historyStart)} UTC: nats-lens stored nothing before then.`;
}

/** What the page says when A is before the history, or there was no collection at A or at B. */
export function collectionNotes(r: Pick<Replay, "a" | "epochAAt" | "epochBAt" | "historyStart">): string[] {
  const out: string[] = [];
  const before = beforeHistoryNote(r);
  if (before) out.push(before);
  const why = "none started in the three collect intervals before it, so the counts that need it are not observed.";
  if (r.epochAAt === undefined) out.push(`No collection at A: ${why}`);
  if (r.epochBAt === undefined) out.push(`No collection at B: ${why}`);
  return out;
}

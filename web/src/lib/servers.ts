// Servers page rules: which servers need attention and why, problems-first
// order, cluster groups, and the summaries the headline draws.
import type { ServerRow } from "../api/servers";

/** A store at this share of its limit or more needs attention. */
export const STORE_ATTENTION_PCT = 60;

/** Name order that puts nats-2 before nats-10. */
export const byName = (a: { name: string }, b: { name: string }) => a.name.localeCompare(b.name, "en", { numeric: true });

export function median(values: number[]): number | undefined {
  if (values.length === 0) return undefined;
  const s = [...values].sort((a, b) => a - b);
  const mid = Math.floor(s.length / 2);
  return s.length % 2 ? s[mid] : (s[mid - 1] + s[mid]) / 2;
}

/** JetStream store in use as a percentage of its limit; unset without both. */
export function storePct(s: ServerRow): number | undefined {
  if (s.jsStorageBytes === undefined || !s.jsMaxStorageBytes) return undefined;
  return (s.jsStorageBytes / s.jsMaxStorageBytes) * 100;
}

/** Stream and consumer leaders on the server; unset when neither was reported. */
export function leaders(s: ServerRow): number | undefined {
  if (s.streamLeaders === undefined && s.consumerLeaders === undefined) return undefined;
  return (s.streamLeaders ?? 0) + (s.consumerLeaders ?? 0);
}

/** Versions by number of servers, most first; the empty version is left out. */
export function versionCounts(servers: ServerRow[]): { version: string; count: number }[] {
  const m = new Map<string, number>();
  for (const s of servers) if (s.version) m.set(s.version, (m.get(s.version) ?? 0) + 1);
  return [...m.entries()].map(([version, count]) => ({ version, count })).sort((a, b) => b.count - a.count || b.version.localeCompare(a.version, "en", { numeric: true }));
}

/** The version most servers run; unset when two versions tie for most. */
export function majorityVersion(servers: ServerRow[]): string | undefined {
  const c = versionCounts(servers);
  if (c.length === 0 || (c.length > 1 && c[0].count === c[1].count)) return undefined;
  return c[0].version;
}

/**
 * Why a server needs attention, most serious first: it did not answer, a
 * warning or critical finding fires on it (info and muted ones do not count), its healthz is not ok,
 * its version is not the majority's, or its store is at 60% or more.
 */
export function attentionReasons(s: ServerRow, majority: string | undefined): string[] {
  const out: string[] = [];
  if (!s.present) out.push("did not answer");
  if (s.status === "critical" || s.status === "warning") out.push(`${s.status} finding`);
  if (s.present && s.healthz && s.healthz !== "ok") out.push(`healthz ${s.healthz}`);
  if (majority !== undefined && s.version && s.version !== majority) out.push(`version ${s.version}`);
  const pct = storePct(s);
  if (pct !== undefined && pct >= STORE_ATTENTION_PCT) out.push(`store ${Math.round(pct)}%`);
  return out;
}

/** Notices about a server that are not reasons for attention: an info finding. */
export function serverNotes(s: ServerRow): string[] {
  return s.status === "info" ? ["info finding"] : [];
}

export function needsAttention(servers: ServerRow[]): ServerRow[] {
  const v = majorityVersion(servers);
  return servers.filter((s) => attentionReasons(s, v).length > 0);
}

/** Lower is worse: not answering, then findings by severity, then other reasons, then info findings, then the rest. */
export function problemRank(s: ServerRow, majority: string | undefined): number {
  if (!s.present) return 0;
  if (s.status === "critical") return 1;
  if (s.status === "warning") return 2;
  if (attentionReasons(s, majority).length > 0) return 3;
  return s.status === "info" ? 4 : 5;
}

export function sortProblemsFirst(servers: ServerRow[], majority: string | undefined): ServerRow[] {
  return [...servers].sort((a, b) => problemRank(a, majority) - problemRank(b, majority) || byName(a, b));
}

export interface ClusterGroup {
  cluster: string;
  rows: ServerRow[];
}

/**
 * Servers by cluster, each group problems first, the group with the worst
 * server first. Whether to show the groups or a flat list is the caller's
 * choice, made from the unfiltered servers.
 */
export function groupByCluster(servers: ServerRow[], majority = majorityVersion(servers)): ClusterGroup[] {
  const m = new Map<string, ServerRow[]>();
  for (const s of servers) m.set(s.cluster, [...(m.get(s.cluster) ?? []), s]);
  const groups = [...m.entries()].map(([cluster, rows]) => ({ cluster, rows: sortProblemsFirst(rows, majority) }));
  const worst = (g: ClusterGroup) => problemRank(g.rows[0], majority);
  return groups.sort((a, b) => worst(a) - worst(b) || a.cluster.localeCompare(b.cluster));
}

/** One cluster, or none named, gives a flat list. */
export function isFlat(servers: ServerRow[]): boolean {
  return new Set(servers.map((s) => s.cluster)).size <= 1;
}

/** Clusters by name, each with its servers by name, for the map. */
export function clustersByName(servers: ServerRow[]): { cluster: string; rows: ServerRow[] }[] {
  const m = new Map<string, ServerRow[]>();
  for (const s of servers) m.set(s.cluster, [...(m.get(s.cluster) ?? []), s]);
  return [...m.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([cluster, rows]) => ({ cluster, rows: [...rows].sort(byName) }));
}

/** Whether a filter text matches the server's name, version or a tag. */
export function matchesText(s: ServerRow, text: string): boolean {
  const t = text.trim().toLowerCase();
  if (!t) return true;
  return s.name.toLowerCase().includes(t) || s.version.toLowerCase().includes(t) || s.tags.some((x) => x.toLowerCase().includes(t));
}

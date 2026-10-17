// Findings grouped into cause cards: findings that name one cause event
// form one group; the others group by check code and parent entity, so
// four consumers of one stream with the same finding are one card.
import type { Entity, Finding, SeverityLevel } from "../api/model";

export interface CauseGroup {
  key: string;
  severity: SeverityLevel;
  /** The check codes in the group, in the order first seen. */
  checkCodes: string[];
  cause?: Finding["cause"];
  /** What every entity shares: a stream, an account, or the system; unset when they differ. */
  parent?: { kind: "stream" | "account" | "system"; name: string };
  /** Number of distinct parents of the entities. */
  parentCount: number;
  findings: Finding[];
}

const SEV_ORDER: Record<SeverityLevel, number> = { critical: 0, warning: 1, info: 2 };

/** The entity a finding's entity belongs to, as a grouping key. */
export function parentKey(e: Entity | undefined): string {
  if (e?.kind === "consumer") return `stream:${e.account}/${e.stream}`;
  if (e?.kind === "stream") return `account:${e.account}`;
  return "system";
}

function parentOf(e: Entity | undefined): CauseGroup["parent"] {
  if (e?.kind === "consumer") return { kind: "stream", name: e.stream };
  if (e?.kind === "stream") return { kind: "account", name: e.accountName || e.account };
  return { kind: "system", name: "" };
}

/**
 * The unmuted firing findings in groups, worst severity first and then the
 * largest group; and the muted ones, in the order given. Resolved findings
 * are left out.
 */
export function groupCauses(findings: Finding[]): { groups: CauseGroup[]; muted: Finding[] } {
  const live = findings.filter((f) => f.state !== "resolved");
  const muted = live.filter((f) => f.mute);
  const byKey = new Map<string, CauseGroup>();
  for (const f of live.filter((x) => !x.mute)) {
    const key = f.cause?.id ? `cause:${f.cause.id}` : `check:${f.checkCode}|${parentKey(f.entity)}`;
    const g = byKey.get(key);
    if (!g) {
      byKey.set(key, { key, severity: f.severity, checkCodes: [f.checkCode], cause: f.cause, parent: parentOf(f.entity), parentCount: 1, findings: [f] });
      continue;
    }
    g.findings.push(f);
    if (!g.checkCodes.includes(f.checkCode)) g.checkCodes.push(f.checkCode);
    if (SEV_ORDER[f.severity] < SEV_ORDER[g.severity]) g.severity = f.severity;
  }
  // A cause group can span parents; it names one only when all share it.
  for (const g of byKey.values()) {
    g.parentCount = new Set(g.findings.map((f) => parentKey(f.entity))).size;
    if (g.parentCount > 1) g.parent = undefined;
  }
  const groups = [...byKey.values()]
    .map((g, i) => ({ g, i }))
    .sort((a, b) => SEV_ORDER[a.g.severity] - SEV_ORDER[b.g.severity] || b.g.findings.length - a.g.findings.length || a.i - b.i)
    .map((x) => x.g);
  return { groups, muted };
}

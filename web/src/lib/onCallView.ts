import type { Finding } from "../api/model";
import type { OnCallAsset, OnCallNow } from "../api/oncall";
import { entityName } from "./view";
export function headline(f: Finding, count: number): string {
  if (count > 1) return `${count} ${f.entity?.kind === "unknown" || !f.entity ? "entities" : `${f.entity.kind}s`}: ${f.checkName}`;
  const name = entityName(f.entity);
  return `${f.checkName}${name ? `: ${name}${f.entity?.kind === "consumer" ? ` on ${f.entity.stream}` : ""}` : ""}`;
}
export function rightNow(n: OnCallNow): string {
  const clauses: string[] = [];
  if (n.openWindow) clauses.push(n.openWindow.label);
  if (n.serversAnswering !== undefined) clauses.push(`${n.serversAnswering}${n.serversExpected !== undefined ? ` of ${n.serversExpected}` : ""} servers answer`);
  if (n.deliveredPerSecond !== undefined) clauses.push(`consumers deliver ${rate(n.deliveredPerSecond)} msgs/s`);
  if (n.streamMsgsPerSecond !== undefined) clauses.push(`streams receive ${rate(n.streamMsgsPerSecond)} msgs/s`);
  return clauses.length ? `${clauses.join("; ")}.` : "Present observations are not reported.";
}
function rate(n: number) { return n.toLocaleString("en-US", { maximumFractionDigits: 1 }); }
export function assetStatus(a: Pick<OnCallAsset, "replicas" | "online" | "leader" | "answering">): string {
  if (a.answering !== undefined) return a.answering ? "Answering" : "Not answering";
  const placement = a.online !== undefined ? `${a.online}${a.replicas !== undefined ? ` of ${a.replicas}` : ""} online` : a.replicas !== undefined ? `Online count not reported · ${a.replicas} configured replicas` : "Placement not reported";
  return `${placement}${a.leader ? ` · leader ${a.leader}` : ""}`;
}
export function contextText(w?: { label: string }): string { return w ? `During a ${w.label}` : ""; }
export function storyNote(shown: number, total: number, note: string): string { return [shown < total ? `Showing ${shown} of ${total} steps.` : "", note].filter(Boolean).join(" "); }
// ASVS 1.2.2: allow only HTTPS for a configured external destination.
export function safePlaybook(url: string): string | undefined {
  try { return new URL(url).protocol === "https:" ? url : undefined; } catch { return undefined; }
}
/** Keep the check's words; split sentence boundaries only outside inline code. */
export function remediationParts(text: string): { warning: string; steps: string[] } {
  const parts: string[] = [];
  let start = 0, code = false;
  for (let i = 0; i < text.length; i++) {
    if (text[i] === "`") code = !code;
    if (!code && /[.!?]/.test(text[i]) && /^\s+[A-Z0-9]/.test(text.slice(i + 1))) {
      parts.push(text.slice(start, i + 1).trim()); start = i + 1;
    }
  }
  const tail = text.slice(start).trim();
  if (tail) parts.push(tail);
  return { warning: parts[0] ?? "", steps: parts.slice(1) };
}

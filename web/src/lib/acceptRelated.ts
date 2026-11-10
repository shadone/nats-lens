// The entities an acceptance can also cover in the same action: the other
// firing, unmuted findings of the same check under the same parent, a
// consumer's stream or a stream's account. A cause card groups by the
// same parents, so its entities are among them.
import type { Entity, Finding } from "../api/model";
import type { AcceptTarget } from "../components/AcceptDialog";
import { acceptable } from "./causeEvidence";
import { streamLabel } from "./view";

/** The most entities one AcceptFindings call takes (api.MaxAcceptBatch). */
export const MAX_ACCEPT_BATCH = 100;

/** How ListFindings narrows to the parent: a stream's consumers, or an account's findings. */
export interface RelatedQuery {
  entity?: Entity;
  account?: string;
}

/** The query for the target's siblings; undefined for an entity with no parent that groups them. */
export function relatedQuery(e: Entity): RelatedQuery | undefined {
  if (e.kind === "consumer" && e.account && e.stream)
    return { entity: { kind: "stream", key: `${e.account}/${e.stream}`, name: e.stream, account: e.account, accountName: e.accountName, stream: e.stream, consumer: "", server: "" } };
  if (e.kind === "stream" && e.account) return { account: e.account };
  return undefined;
}

function sameParent(a: Entity, b: Entity): boolean {
  if (a.kind !== b.kind || a.account !== b.account) return false;
  return a.kind === "consumer" ? a.stream === b.stream : a.kind === "stream";
}

/** The findings of the same check under the same parent that can still be accepted, without the target. */
export function relatedFindings(fs: Finding[], checkCode: string, target: Entity, targetId: string): Finding[] {
  return fs.filter((f) => f.id !== targetId && f.checkCode === checkCode && f.state !== "resolved" && acceptable(f) && sameParent(f.entity!, target));
}

/**
 * The text with each account ID of the entities written as the account's
 * label: the server names an entity by its key, which starts with the
 * account's ID.
 */
export function withAccountNames(text: string, es: Entity[], label: (id: string, name?: string) => string): string {
  const names = new Map<string, string>();
  for (const e of es) if (e.account && e.accountName) names.set(e.account, label(e.account, e.accountName));
  let out = text;
  for (const [id, name] of names) out = out.split(id).join(name);
  return out;
}

/** "other consumers of stream order-jobs", "other streams of account APP". */
export function parentPhrase(e: Entity, accountLabel: (id: string, name?: string) => string): string {
  if (e.kind === "consumer") return `other consumers of stream ${streamLabel(e.stream)}`;
  return `other streams of account ${accountLabel(e.account, e.accountName)}`;
}

/** The acceptance of one finding; preselect are related findings to offer ticked. */
export function acceptTarget(f: Finding, preselect?: Finding[]): AcceptTarget {
  return { findingId: f.id, checkCode: f.checkCode, checkName: f.checkName, entity: f.entity!, growthReview: !!f.growthReview, ...(preselect ? { preselect } : {}) };
}

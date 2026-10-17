// Labels, ordering and routes derived from view models.
import type { Entity, EventSourceName, Finding, Level } from "../api/model";

export const LEVEL_LABEL: Record<Level, string> = {
  critical: "Critical",
  warning: "Warning",
  info: "Info",
  muted: "Muted",
  ok: "OK",
  nodata: "No data",
};

/** Lower sorts first. */
export const LEVEL_ORDER: Record<Level, number> = { critical: 0, warning: 1, info: 2, muted: 3, ok: 4, nodata: 5 };

/**
 * Whether a state is trouble: critical, warning, not collecting, or a
 * summary that could not be read. Info is a notice: it is counted and
 * drawn, but never turns an OK label into another.
 */
export function isTrouble(state: Level | "notcollecting" | "unavailable"): boolean {
  return state === "critical" || state === "warning" || state === "notcollecting" || state === "unavailable";
}

/** The transition tag the reference shows next to a finding. */
export type FindingTag = "NEW" | "FLAPPING" | "RESOLVED" | "CHANGED" | "OPEN";

/** A finding is NEW when it started firing inside the range; the API leaves that to the UI. */
export function findingTag(f: Finding, rangeStart: number | undefined): FindingTag {
  switch (f.state) {
    case "flapping":
      return "FLAPPING";
    case "resolved":
      return "RESOLVED";
    case "changed":
      return "CHANGED";
    default:
      return f.since !== undefined && rangeStart !== undefined && f.since >= rangeStart ? "NEW" : "OPEN";
  }
}

/** Text of the source pill, as in the reference. */
export const SOURCE_PILL: Record<EventSourceName, string> = {
  nats: "nats",
  kubernetes: "k8s",
  finding: "check",
  collector: "lens",
  other: "other",
};

/** CSS modifier of the source pill. */
export const SOURCE_CLASS: Record<EventSourceName, string> = {
  nats: "nats",
  kubernetes: "k8s",
  finding: "lens",
  collector: "",
  other: "",
};

const enc = encodeURIComponent;

/** Route of an entity's page, or undefined when it has none. */
export function entityPath(e: Entity | undefined): string | undefined {
  if (!e) return undefined;
  switch (e.kind) {
    case "stream":
      return `/streams/${enc(e.account)}/${enc(e.stream)}`;
    case "consumer":
      return `/consumers/${enc(e.account)}/${enc(e.stream)}/${enc(e.consumer)}`;
    case "server":
      return `/servers/${enc(e.server || e.key)}`;
    case "connection":
      return `/connections?server=${enc(e.server)}`;
    case "client":
      return `/connections?name=${enc(e.key)}`;
    default:
      return undefined;
  }
}

export function streamPath(account: string, stream: string): string {
  return `/streams/${enc(account)}/${enc(stream)}`;
}

export function consumerPath(account: string, stream: string, consumer: string): string {
  return `/consumers/${enc(account)}/${enc(stream)}/${enc(consumer)}`;
}

export function serverPath(name: string): string {
  return `/servers/${enc(name)}`;
}

export function findingPath(id: string): string {
  return `/findings/${enc(id)}`;
}

/** The events page opened on one event. */
export function eventPath(id: string): string {
  return `/events?id=${enc(id)}`;
}

/** A stream's consumers as "present/expected", with "–" for an unknown
 * count; short only when both are known and fewer are present. */
export function consumerCount(s: { consumers?: number; consumersExpected?: number }): { text: string; short: boolean } {
  const present = s.consumers === undefined ? "–" : String(s.consumers);
  const short = s.consumers !== undefined && s.consumersExpected !== undefined && s.consumers < s.consumersExpected;
  return { text: s.consumersExpected !== undefined ? `${present}/${s.consumersExpected}` : present, short };
}

/** An account's readable name, else its ID. */
export function accountLabel(id: string, name?: string): string {
  return name || id;
}

/** A known account: its ID and, when known, its readable name. */
export interface AccountRef {
  account: string;
  accountName?: string;
}

/** The label of each account in a loaded set: its name, or its ID without
 * one; a name two IDs share gets an ID prefix, "APP (ACJQ…)", so the
 * accounts can be told apart. */
export function accountLabeller(refs: AccountRef[]): (id: string, name?: string) => string {
  const ids = new Map<string, Set<string>>();
  for (const r of refs) {
    if (!r.accountName || !r.account) continue;
    const set = ids.get(r.accountName) ?? new Set<string>();
    set.add(r.account);
    ids.set(r.accountName, set);
  }
  return (id, name) => {
    if (!name) return id;
    return (ids.get(name)?.size ?? 0) > 1 ? `${name} (${id.slice(0, 4)}…)` : name;
  };
}

/** An entity's key with the account ID replaced by its label. */
export function entityKeyLabel(e: Entity, label: (id: string, name?: string) => string = accountLabel): string {
  if (!e.accountName || !e.account) return e.key;
  const acc = label(e.account, e.accountName);
  if (e.key === e.account) return acc;
  return e.key.startsWith(e.account + "/") ? acc + e.key.slice(e.account.length) : e.key;
}

/** The account name of id among refs, for an entity built from a URL. */
export function withAccountName<T extends AccountRef>(e: T, refs: (AccountRef | undefined)[]): T {
  if (e.accountName) return e;
  const found = refs.find((r) => r && r.account === e.account && r.accountName);
  return found ? { ...e, accountName: found.accountName } : e;
}

/** Display name of an entity: the consumer, stream, server or key. */
export function entityName(e: Entity | undefined): string {
  if (!e) return "";
  return e.name || e.consumer || e.stream || e.server || e.key;
}

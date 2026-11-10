// The command bar's results: one group per kind, in a fixed order, each
// capped, with a row that shows more when the group has more.
import type { Finding, LensEvent, SeverityLevel } from "../api/model";
import { searchItems, type SearchItem } from "../api/search";
import { COMMAND_KINDS, type CommandKind, type CommandQuery } from "./commandQuery";
import { dayMonHm, hm } from "./format";
import { eventPath, findingPath } from "./view";

export const GROUP_LABEL: Record<CommandKind, string> = {
  server: "Servers",
  stream: "Streams",
  consumer: "Consumers",
  kv: "KV stores",
  object: "Object stores",
  account: "Accounts",
  finding: "Findings",
  event: "Events",
};

/** The page that lists every item of a kind. */
export const KIND_PAGE: Record<CommandKind, string> = {
  server: "/servers",
  stream: "/streams",
  consumer: "/consumers",
  kv: "/kv-stores",
  object: "/object-stores",
  account: "/accounts",
  finding: "/findings",
  event: "/events",
};

/** Rows per group when every kind shows, and when one kind does. */
export const GROUP_CAP = 4;
export const NARROWED_CAP = 20;
export const REMOTE_CAP = 5;
export const NARROWED_REMOTE_CAP = 10;

export type RemoteState = "idle" | "loading" | "error" | "ready";

export interface ResultRow {
  /** Unique within the results. */
  key: string;
  kind: CommandKind;
  /** The part read first: a name, or a finding's check code. */
  name: string;
  /** Read after the name: a finding's entity, an event's summary. */
  detail?: string;
  /** Right-hand note: an account, a stream, a severity and time. */
  note: string;
  severity?: SeverityLevel;
  path: string;
}

/** What the last row of a group does when the group has more. */
export type MoreAction = { kind: "narrow"; to: CommandKind } | { kind: "page"; path: string };

export interface ResultGroup {
  kind: CommandKind;
  label: string;
  rows: ResultRow[];
  /** Matches in all, where known. */
  total?: number;
  more?: MoreAction;
  state: RemoteState;
  /** The rows answer the previous text. */
  stale?: boolean;
}

const SEVERITY_LABEL: Record<SeverityLevel, string> = { critical: "Critical", warning: "Warning", info: "Info" };

/** A time as hh:mm within a day of now, with the date before that. */
export function whenText(ms: number, now: number): string {
  return now - ms < 86_400_000 && ms <= now ? hm(ms) : dayMonHm(ms);
}

function findingNote(f: Finding, now: number): string {
  const parts = [SEVERITY_LABEL[f.severity]];
  if (f.mute) parts.push(f.mute.accepted ? "accepted" : "muted");
  if (f.state === "resolved" && f.resolvedAt) parts.push(`resolved ${whenText(f.resolvedAt, now)}`);
  else if (f.since) parts.push(`${f.state === "flapping" ? "flapping" : "firing"} since ${whenText(f.since, now)}`);
  return parts.join(" · ");
}

export function findingRow(f: Finding, now: number): ResultRow {
  const e = f.entity;
  return {
    key: `finding:${f.id}`,
    kind: "finding",
    name: f.checkCode,
    detail: e ? e.consumer || e.name || e.key : f.checkName,
    note: findingNote(f, now),
    severity: f.severity,
    path: findingPath(f.id),
  };
}

/** An event row; a folded row (repeats above 1) shows its count before the newest time. */
export function eventRow(e: LensEvent & { repeats?: number }, now: number): ResultRow {
  const subject = e.consumer || e.stream || e.clientName || e.server;
  const when = e.time !== undefined ? whenText(e.time, now) : "";
  return {
    key: `event:${e.id}`,
    kind: "event",
    name: subject || e.type,
    detail: subject ? e.type : e.summary,
    note: (e.repeats ?? 1) > 1 ? [`×${e.repeats}`, when].filter(Boolean).join(" · ") : when,
    severity: e.severity,
    path: eventPath(e.id),
  };
}

function nameRow(i: SearchItem): ResultRow {
  return { key: `${i.kind}:${i.path}`, kind: i.kind, name: i.name, note: i.sub, path: i.path };
}

function moreFor(kind: CommandKind, narrowed: boolean): MoreAction {
  return narrowed ? { kind: "page", path: KIND_PAGE[kind] } : { kind: "narrow", to: kind };
}

export interface RemoteResult<T> {
  state: RemoteState;
  items: T[];
  total: number;
  /** The server has a next page. */
  hasMore?: boolean;
  /** The items answer the previous text and show while the next ones load. */
  stale?: boolean;
}

/**
 * The groups to show, in the order of COMMAND_KINDS. A name group shows
 * when it has a match; the findings and events groups show while they
 * load, fail or have a match.
 */
export function groupResults(
  q: CommandQuery,
  names: SearchItem[],
  findings: RemoteResult<Finding>,
  events: RemoteResult<LensEvent & { repeats?: number }>,
  now: number,
): ResultGroup[] {
  const narrowed = q.kind !== undefined;
  const cap = narrowed ? NARROWED_CAP : GROUP_CAP;
  // A kind prefix alone lists that kind by name.
  const matched = q.text
    ? searchItems(names, q.text, Number.POSITIVE_INFINITY)
    : narrowed
      ? names.filter((i) => i.kind === q.kind).sort((a, b) => a.name.localeCompare(b.name))
      : [];
  const out: ResultGroup[] = [];
  for (const kind of COMMAND_KINDS) {
    if (narrowed && q.kind !== kind) continue;
    if (kind === "finding" || kind === "event") {
      const r = kind === "finding" ? findings : events;
      if (r.state === "idle" || (r.state === "ready" && r.items.length === 0)) continue;
      const remoteCap = narrowed ? NARROWED_REMOTE_CAP : REMOTE_CAP;
      const rows = r.items.slice(0, remoteCap).map((x) => (kind === "finding" ? findingRow(x as Finding, now) : eventRow(x as LensEvent & { repeats?: number }, now)));
      out.push({
        kind,
        label: GROUP_LABEL[kind],
        rows,
        total: r.state === "ready" ? r.total : undefined,
        more: r.state === "ready" && (r.hasMore || r.items.length > rows.length) ? moreFor(kind, narrowed) : undefined,
        state: r.state,
        stale: r.stale,
      });
      continue;
    }
    const all = matched.filter((i) => i.kind === kind);
    if (all.length === 0) continue;
    out.push({
      kind,
      label: GROUP_LABEL[kind],
      rows: all.slice(0, cap).map(nameRow),
      total: all.length,
      more: all.length > cap ? moreFor(kind, narrowed) : undefined,
      state: "ready",
    });
  }
  return out;
}

/** The label of a group's last row, when the group has more. */
export function moreLabel(g: ResultGroup): string {
  if (g.more?.kind === "page") return `Open the ${g.label.toLowerCase().replace(/^kv/, "KV")} page`;
  return g.total !== undefined ? `Show all ${g.total}` : "Show all";
}

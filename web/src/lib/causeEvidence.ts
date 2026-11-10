// The evidence of a cause card: which servers hold the affected assets,
// what that says at a glance, a nats CLI command that checks the same
// condition, and which findings may be accepted as by design.
import type { AssetPlacement } from "../api/overview";
import type { Entity, Finding } from "../api/model";
import type { PlacementStateName } from "../api/adapt-placement";
import { CARD_SHOWN, type CauseGroup } from "./causeGroups";
import { natsCommand } from "./natsCommand";

/** The findings a card lists, the ones its evidence covers. */
export function shownFindings(g: CauseGroup): Finding[] {
  return g.findings.slice(0, CARD_SHOWN);
}

function placed(e: Entity | undefined): e is Entity {
  return e?.kind === "stream" || e?.kind === "consumer";
}

/** The streams and consumers the cards list, once each, in card order: what one placement read asks for. */
export function evidenceEntities(groups: CauseGroup[], limit: number): Entity[] {
  const seen = new Set<string>();
  const out: Entity[] = [];
  for (const g of groups)
    for (const f of shownFindings(g)) {
      const e = f.entity;
      if (!placed(e) || seen.has(`${e.kind}:${e.key}`) || out.length >= limit) continue;
      seen.add(`${e.kind}:${e.key}`);
      out.push(e);
    }
  return out;
}

/** An entity's key across kinds: "stream:APP/orders". */
function entityKey(e: Entity | undefined): string {
  return `${e?.kind ?? "system"}:${e?.key ?? ""}`;
}

/** The findings with one per entity, the first of each: what the evidence counts. */
export function distinctEntities(findings: Finding[]): Finding[] {
  const seen = new Set<string>();
  return findings.filter((f) => !seen.has(entityKey(f.entity)) && !!seen.add(entityKey(f.entity)));
}

/** One affected asset on a server: the entity, its findings, and the replica's state ("server" for the server's own findings, "connection" for a connection on it). */
export interface ServerAsset {
  entity: Entity;
  findings: Finding[];
  state: PlacementStateName | "server" | "connection";
}

/** A server with the affected assets it holds, each once. */
export interface ServerRow {
  server: string;
  assets: ServerAsset[];
  leads: number;
  /** Replicas that are not the leader and not in sync: behind, offline, missing, disagreeing, without a leader. */
  troubled: number;
}

const TROUBLED: PlacementStateName[] = ["behind", "offline", "missing", "disagrees", "unknown"];

/** Worst first: a troubled replica, then the leader, then the rest. */
function stateRank(s: ServerAsset["state"]): number {
  const t = (TROUBLED as string[]).indexOf(s);
  return t >= 0 ? t : s === "leader" ? 10 : 20;
}

/**
 * The servers that hold the findings' assets, most troubled first, then
 * the one that leads the most, then the one that holds the most, then by
 * name; and the streams and consumers the placement reports no server
 * holds in the newest collection. Each server lists an entity once,
 * however many findings it has, with its worst state there. A server
 * finding is on its server, a connection's on the server it is connected
 * to; accounts, clients and the system are on none.
 */
export function serverRows(findings: Finding[], placement: Map<string, AssetPlacement>): { rows: ServerRow[]; unplaced: Finding[] } {
  const byServer = new Map<string, ServerRow>();
  const unplaced: Finding[] = [];
  const add = (server: string, f: Finding, state: ServerAsset["state"]) => {
    const r = byServer.get(server) ?? { server, assets: [], leads: 0, troubled: 0 };
    const a = r.assets.find((x) => entityKey(x.entity) === entityKey(f.entity));
    if (!a) r.assets.push({ entity: f.entity!, findings: [f], state });
    else {
      a.findings.push(f);
      if (stateRank(state) < stateRank(a.state)) a.state = state;
    }
    r.leads = r.assets.filter((x) => x.state === "leader").length;
    r.troubled = r.assets.filter((x) => (TROUBLED as string[]).includes(x.state)).length;
    byServer.set(server, r);
  };
  for (const f of findings) {
    const e = f.entity;
    if (e?.kind === "server") add(e.server || e.key, f, "server");
    else if (e?.kind === "connection" && e.server) add(e.server, f, "connection");
    else if (placed(e)) {
      // An asset the answer does not hold, as while a placeholder stands in, is neither placed nor unplaced.
      const p = placement.get(e.key);
      if (!p) continue;
      if (!p.reported) {
        if (!unplaced.some((u) => entityKey(u.entity) === entityKey(e))) unplaced.push(f);
      } else for (const r of p.replicas) if (r.server) add(r.server, f, r.state);
    }
  }
  const rows = [...byServer.values()].sort(
    (a, b) => b.troubled - a.troubled || b.leads - a.leads || b.assets.length - a.assets.length || a.server.localeCompare(b.server),
  );
  return { rows, unplaced };
}

/** The distinct entities the rows hold, and of them the streams and consumers, which have replicas. */
export function heldCounts(rows: ServerRow[]): { held: number; replicated: number } {
  const all = new Set<string>();
  const rep = new Set<string>();
  for (const r of rows)
    for (const a of r.assets) {
      all.add(entityKey(a.entity));
      if (a.state !== "server" && a.state !== "connection") rep.add(entityKey(a.entity));
    }
  return { held: all.size, replicated: rep.size };
}

const TROUBLE_WORDS: Record<string, [string, string]> = {
  behind: ["behind its leader", "behind their leaders"],
  offline: ["offline", "offline"],
  missing: ["missing", "missing"],
  disagrees: ["disagreeing with its leader", "disagreeing with their leaders"],
  unknown: ["without a leader", "without a leader"],
};

/**
 * One sentence on what the placement shows, when one server stands out
 * for two assets or more: "nats-us-4 holds a replica of 2 of 2 streams,
 * offline.", "nats-eu-3 leads 4 of 4 consumers.", or "All 3 streams have
 * a replica on nats-1." while another server holds fewer. placedCount is
 * the distinct streams and consumers placed; when the card lists more
 * (listed), the count says "of the 3 placed". Undefined when nothing
 * stands out.
 */
export function placementSummary(rows: ServerRow[], placedCount: number, noun: string, listed = placedCount): string | undefined {
  if (placedCount < 2) return undefined;
  const top = rows.find((r) => r.assets.some((a) => a.state !== "server" && a.state !== "connection"));
  if (!top) return undefined;
  const of = placedCount < listed ? `of the ${placedCount} placed ${noun}` : `of ${placedCount} ${noun}`;
  const t = rows.reduce((best, r) => (r.troubled > best.troubled ? r : best), top);
  if (t.troubled >= 2) {
    const states = [...new Set(t.assets.filter((a) => (TROUBLED as string[]).includes(a.state)).map((a) => a.state))];
    const word = states.length === 1 ? TROUBLE_WORDS[states[0]][1] : "not in sync";
    return `${t.server} holds a replica of ${t.troubled} ${of}, ${word}.`;
  }
  const l = rows.reduce((best, r) => (r.leads > best.leads ? r : best), top);
  if (l.leads >= 2 && l.leads * 2 > placedCount) return `${l.server} leads ${l.leads} ${of}.`;
  const replicas = (r: ServerRow) => r.assets.filter((a) => a.state !== "server" && a.state !== "connection").length;
  const all = rows.find((r) => replicas(r) === placedCount);
  if (all && rows.some((r) => replicas(r) > 0 && replicas(r) < placedCount))
    return placedCount < listed ? `All ${placedCount} placed ${noun} have a replica on ${all.server}.` : `All ${placedCount} ${noun} have a replica on ${all.server}.`;
  return undefined;
}

/** A command to copy, with the nats context it needs. */
export interface CliCheck {
  command: string | undefined;
  note: string;
}

function contextFor(account: string | undefined, accountName: string | undefined, system: string): string {
  const where = system || "the default system";
  if (account === undefined) return `Run with a nats context for the system account on ${where}.`;
  const acc = accountName && accountName !== account ? `${accountName} (${account})` : account;
  return `Run with a nats context for account ${acc} on ${where}.`;
}

/**
 * The nats CLI command that shows what the card's findings report, for
 * the entities it lists: a stream's or a consumer's info for one, the
 * consumer report of their stream or the stream report for several, a
 * server's info, the account's info, the JetStream report for the system,
 * the connections report for clients. Undefined for entities of several
 * kinds or of several accounts, which no one command and context cover.
 * The commands name no account, user or credentials: the context does.
 */
export function cliCheck(findings: Finding[], system: string): CliCheck | undefined {
  const es = findings.map((f) => f.entity);
  const kinds = new Set(es.map((e) => e?.kind ?? "system"));
  if (kinds.size !== 1) return undefined;
  const kind = [...kinds][0];
  const first = es[0];
  if (kind === "stream" || kind === "consumer" || kind === "account") {
    const accounts = new Set(es.map((e) => e!.account));
    if (accounts.size !== 1 || !first) return undefined;
    const note = contextFor(first.account, first.accountName, system);
    if (kind === "account") return { command: "nats account info", note };
    if (kind === "stream")
      return { command: es.length === 1 ? natsCommand(["stream", "info"], [first.stream], ["--json"]) : "nats stream report", note };
    const streams = new Set(es.map((e) => e!.stream));
    if (es.length === 1) return { command: natsCommand(["consumer", "info"], [first.stream, first.consumer], ["--json"]), note };
    return { command: streams.size === 1 ? natsCommand(["consumer", "report"], [first.stream]) : "nats stream report", note };
  }
  const note = contextFor(undefined, undefined, system);
  if (kind === "server") return { command: es.length === 1 && first ? natsCommand(["server", "info"], [first.server || first.key]) : "nats server list", note };
  if (kind === "client" || kind === "connection") return { command: "nats server report connections", note };
  if (kind === "system") return { command: "nats server report jetstream", note };
  return undefined;
}

/**
 * Whether the Overview offers to accept the finding as by design: its
 * check describes a configuration choice (the catalog's acceptable, which
 * the API enforces), it names one entity, and it is not muted already.
 */
export function acceptable(f: Finding): boolean {
  return !!f.acceptable && !!f.entity && f.entity.kind !== "system" && f.entity.kind !== "unknown" && !f.mute;
}

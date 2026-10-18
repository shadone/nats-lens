// What the Placement page shows of a cell: its text, its tone, what it
// means, the commands to check it by hand, and how it changed since a
// moment to compare with.
import type { PlacementStateName } from "../api/adapt-placement";
import type { Matrix, MatrixAsset, MatrixCell } from "../api/placementMatrix";
import { assetsOf } from "../api/placementMatrix";
import { dayMonHm, fmtInt } from "./format";
import { natsCommand } from "./natsCommand";
import type { RangeSpec } from "./timeRange";

/** A server that holds no replica of an asset. */
export type CellState = PlacementStateName | "not_placed";

export const CELL_LABEL: Record<CellState, string> = {
  leader: "leader",
  in_sync: "in sync",
  behind: "behind",
  disagrees: "disagrees",
  missing: "missing",
  offline: "offline",
  unknown: "state unknown",
  not_placed: "not placed",
};

export function isConsumer(a: MatrixAsset): boolean {
  return a.entity.kind === "consumer";
}

/** "MM-DD HH:MM" in UTC. */
function monthDayHm(ms: number): string {
  return new Date(ms).toISOString().slice(5, 16).replace("T", " ");
}

/** The cell's text: what differs, else the replica's lag or position. */
export function cellText(a: MatrixAsset, c: MatrixCell): string {
  if (c.differs.includes("created") && c.created !== undefined) return `created ${monthDayHm(c.created)}`;
  if (c.differs.includes("config_hash")) return "config differs";
  switch (c.state) {
    case "missing":
      return "reports nothing";
    case "offline":
      return "offline";
    case "unknown":
      return "no leader";
    case "disagrees":
      return "not in leader's list";
    case "leader":
      if (!isConsumer(a) && c.lastSeq !== undefined) return `last seq ${fmtInt(c.lastSeq)}`;
      if (isConsumer(a) && c.delivered !== undefined) return `delivered ${fmtInt(c.delivered)}`;
      return "leader";
  }
  return c.lag !== undefined ? `lag ${fmtInt(c.lag)}` : CELL_LABEL[c.state];
}

/** The CSS tone of a cell: a disagreement shows whatever the state. */
export function cellTone(c: MatrixCell | undefined): string {
  if (!c) return "none";
  if (c.state === "offline") return "offline";
  if (c.state === "missing") return "missing";
  if (c.state === "unknown") return "unknown";
  if (c.state === "disagrees" || c.differs.length > 0) return "disagree";
  if (c.state === "behind") return "behind";
  return "ok";
}

/** L for the leader, F for a follower, nothing for a replica without a role. */
export function roleLetter(c: MatrixCell): string {
  if (c.state === "leader") return "L";
  return c.state === "missing" || c.state === "offline" || c.state === "unknown" ? "" : "F";
}

/** One sentence on what the cell's state means. */
export function meaning(a: MatrixAsset, c: MatrixCell): string {
  const what = isConsumer(a) ? "consumer" : "stream";
  const parts: string[] = [];
  if (c.differs.includes("created")) {
    parts.push(
      `This server records another creation time for the ${what} than most servers do: its copy was created again, for example when the replica was rebuilt or the ${what} deleted and added back.`,
    );
  }
  if (c.differs.includes("config_hash")) parts.push(`This server holds another configuration of the ${what} than most servers do: an update did not reach it.`);
  switch (c.state) {
    case "leader":
      parts.push(`This server leads the ${what}: the other replicas follow its log.`);
      break;
    case "in_sync":
      parts.push("The leader reports this replica as current.");
      break;
    case "behind":
      parts.push(`The leader reports this replica as not current${c.lag !== undefined ? `, ${fmtInt(c.lag)} raft entries behind` : ""}; it catches up unless the lag keeps growing.`);
      break;
    case "disagrees":
      if (c.differs.length === 0) parts.push("The leader does not list this server as a replica, but the server still reports one.");
      break;
    case "missing":
      parts.push(`The leader lists this server as a replica, but the server reports no ${what}: it has lost or not yet built its copy.`);
      break;
    case "offline":
      parts.push("The leader lists this replica as offline: its server has not been seen recently.");
      break;
    case "unknown":
      parts.push(`No server led the ${what} in this collection, so nobody says how this replica stands.`);
      break;
  }
  return parts.join(" ");
}

/** The names a command about the asset takes: the stream, and a consumer's name. */
function names(a: MatrixAsset): string[] {
  return isConsumer(a) ? [a.entity.stream, a.entity.consumer] : [a.entity.stream];
}

/**
 * The nats CLI command that shows the asset as its leader reports it;
 * undefined when a name holds a control character.
 */
export function infoCommand(a: MatrixAsset, c?: MatrixCell): string | undefined {
  const cmd = natsCommand([isConsumer(a) ? "consumer" : "stream", "info"], names(a), ["--json"]);
  return cmd && c?.differs.includes("created") ? `${cmd} | jq .created` : cmd;
}

/** The nats CLI command that asks the leader to step down; nats-lens never runs it. */
export function stepDownCommand(a: MatrixAsset): string | undefined {
  return natsCommand([isConsumer(a) ? "consumer" : "stream", "cluster", "step-down"], names(a));
}

/** Whether every server that names peers names the same replica set, itself included. */
export function peersAgree(a: MatrixAsset): boolean {
  const sets = a.cells.filter((c) => c.peers.length > 0).map((c) => [...c.peers, c.server].sort().join(" "));
  return sets.every((s) => s === sets[0]);
}

/** The asset's state on a server, not_placed when it holds no replica. */
export function stateOn(a: MatrixAsset | undefined, server: string): CellState {
  return a?.cells.find((c) => c.server === server)?.state ?? "not_placed";
}

/** How a cell stood at the moment compared with, when that differs from now. */
export interface Change {
  then: CellState;
  /** The asset was not in the matrix then. */
  newAsset: boolean;
}

/**
 * The cells whose state differs between now and then, keyed by
 * "<asset key>|<server>", over every server of either matrix.
 */
export function changes(now: Matrix, then: Matrix): Map<string, Change> {
  const before = new Map(assetsOf(then).map((a) => [a.entity.key, a]));
  const servers = new Set([...now.servers, ...then.servers].map((s) => s.name));
  const out = new Map<string, Change>();
  for (const a of assetsOf(now)) {
    const b = before.get(a.entity.key);
    for (const s of servers) {
      const st = stateOn(a, s);
      const was = stateOn(b, s);
      if (st !== was && !(st === "not_placed" && !b)) out.set(`${a.entity.key}|${s}`, { then: was, newAsset: !b });
    }
  }
  return out;
}

/** What the comparison selector names in the URL: "" (none), "1d" or a UTC time. */
export type CompareChoice = "" | "1d" | string;

const ISO = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?Z$/;

/** The moment a choice names, or undefined for none or a malformed one. */
export function compareAt(choice: CompareChoice, end: number): number | undefined {
  if (choice === "1d") return end - 86_400_000;
  if (ISO.test(choice)) return Date.parse(choice);
  return undefined;
}

/** The range whose newest collection stands for the moment: the hour before it. */
export function compareSpec(at: number): RangeSpec {
  return { kind: "fixed", from: at - 3_600_000, to: at };
}

/** "16 Oct 14:30 UTC" */
export function momentText(at: number): string {
  return `${dayMonHm(at)} UTC`;
}

// What the command bar's text asks for. A word such as `stream:` narrows
// the results to one kind, and `since:2h` narrows findings and events to
// the last two hours of the selected range; a word that looks like a
// prefix but is not one stays part of the text.
import type { SearchKind } from "../api/search";

export type CommandKind = SearchKind | "finding" | "event";

/** The prefix of each kind, in the order the results show the kinds. */
export const KIND_PREFIX: Record<CommandKind, string> = {
  server: "server",
  stream: "stream",
  consumer: "consumer",
  kv: "kv",
  object: "object",
  account: "account",
  finding: "finding",
  event: "event",
};

export const COMMAND_KINDS = Object.keys(KIND_PREFIX) as CommandKind[];

const BY_PREFIX = new Map(COMMAND_KINDS.map((k) => [KIND_PREFIX[k], k]));

const UNIT_MS: Record<string, number> = { m: 60_000, h: 3_600_000, d: 86_400_000 };

/** Longest window since: takes, as long as the longest fixed range. */
export const MAX_SINCE_MS = 30 * 86_400_000;

export interface CommandQuery {
  /** The text to match, prefixes removed and spaces collapsed. */
  text: string;
  /** The one kind to show; unset shows every kind. */
  kind?: CommandKind;
  /** The window before the selected range's end that findings and events must fall in. */
  sinceMs?: number;
  /** since: as typed, for example 2h. */
  sinceLabel?: string;
}

/** since: values: a whole number of minutes, hours or days, up to 30 days. */
function sinceOf(v: string): number | undefined {
  const m = /^(\d{1,4})([mhd])$/i.exec(v);
  if (!m) return undefined;
  const ms = Number(m[1]) * UNIT_MS[m[2].toLowerCase()];
  return ms > 0 && ms <= MAX_SINCE_MS ? ms : undefined;
}

/**
 * The words of the text, a kind prefix split from what follows it, so
 * that `event:since:2h` reads as `event:` and `since:2h`.
 */
function words(raw: string): string[] {
  const out: string[] = [];
  for (let word of raw.trim().split(/\s+/)) {
    for (let m = /^([a-z]+):(.+)$/i.exec(word); m && BY_PREFIX.has(m[1].toLowerCase()); m = /^([a-z]+):(.+)$/i.exec(word)) {
      out.push(`${m[1]}:`);
      word = m[2];
    }
    if (word) out.push(word);
  }
  return out;
}

function kindOf(word: string): CommandKind | undefined {
  const m = /^([a-z]+):$/i.exec(word);
  return m ? BY_PREFIX.get(m[1].toLowerCase()) : undefined;
}

function sinceWord(word: string): number | undefined {
  const m = /^since:(.*)$/i.exec(word);
  return m ? sinceOf(m[1]) : undefined;
}

/**
 * Splits the command bar's text into prefixes and the text to match.
 * `stream:order` and `stream: order` both narrow to streams named like
 * order; the last kind prefix wins.
 */
export function parseCommand(raw: string): CommandQuery {
  const q: CommandQuery = { text: "" };
  const rest: string[] = [];
  for (const word of words(raw)) {
    const kind = kindOf(word);
    const since = sinceWord(word);
    if (kind) q.kind = kind;
    else if (since !== undefined) {
      q.sinceMs = since;
      q.sinceLabel = word.slice("since:".length).toLowerCase();
    } else rest.push(word);
  }
  q.text = rest.join(" ");
  return q;
}

/** The text with one kind of prefix taken out, for a chip's remove button. */
export function withoutPrefix(raw: string, prefix: "kind" | "since"): string {
  return words(raw)
    .filter((word) => (prefix === "kind" ? !kindOf(word) : sinceWord(word) === undefined))
    .join(" ");
}

/** The text narrowed to one kind: other kind prefixes go, since: stays. */
export function narrowedTo(raw: string, kind: CommandKind): string {
  const rest = withoutPrefix(raw, "kind");
  return `${KIND_PREFIX[kind]}: ${rest}`.trimEnd() + (rest ? "" : " ");
}

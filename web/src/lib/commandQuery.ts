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
 * Splits the command bar's text into prefixes and the text to match.
 * `stream:order` and `stream: order` both narrow to streams named like
 * order; the last kind prefix wins.
 */
export function parseCommand(raw: string): CommandQuery {
  const q: CommandQuery = { text: "" };
  const words: string[] = [];
  for (const word of raw.trim().split(/\s+/)) {
    const m = /^([a-z]+):(.*)$/i.exec(word);
    const kind = m ? BY_PREFIX.get(m[1].toLowerCase()) : undefined;
    if (m && kind) {
      q.kind = kind;
      if (m[2]) words.push(m[2]);
      continue;
    }
    const since = m && m[1].toLowerCase() === "since" ? sinceOf(m[2]) : undefined;
    if (since !== undefined) {
      q.sinceMs = since;
      q.sinceLabel = m![2].toLowerCase();
      continue;
    }
    if (word) words.push(word);
  }
  q.text = words.join(" ");
  return q;
}

/** The text with one prefix word taken out, for a chip's remove button. */
export function withoutPrefix(raw: string, prefix: "kind" | "since"): string {
  return raw
    .trim()
    .split(/\s+/)
    .flatMap((word) => {
      const m = /^([a-z]+):(.*)$/i.exec(word);
      if (!m) return [word];
      const name = m[1].toLowerCase();
      if (prefix === "kind" && BY_PREFIX.has(name)) return m[2] ? [m[2]] : [];
      if (prefix === "since" && name === "since" && sinceOf(m[2]) !== undefined) return [];
      return [word];
    })
    .join(" ");
}

/** The text narrowed to one kind: other kind prefixes go, since: stays. */
export function narrowedTo(raw: string, kind: CommandKind): string {
  const rest = withoutPrefix(raw, "kind");
  return `${KIND_PREFIX[kind]}: ${rest}`.trimEnd() + (rest ? "" : " ");
}

// "Export as incident log": the replay as plain text, with every piece of
// evidence as an absolute link, and the name of the file it downloads as.
import type { Replay, ReplayEvidence, ReplayStep } from "../api/adapt-replay";
import { isoSecond } from "./format";
import { withGlobal } from "./globalQuery";
import { compactMoment } from "./replayUrl";
import {
  FOOTER,
  KIND_LABEL,
  collectionNotes,
  countTiles,
  evidencePath,
  electionsText,
  evidenceWord,
  groupEvidence,
  goneAtB,
  goneKind,
  gonePath,
  serversText,
  stepNotes,
  stepWhen,
  unobservedText,
  whenFormat,
} from "./replayView";
import { entityName } from "./view";

export interface LogContext {
  /** The system's ID as the page names it; empty for the default system. */
  system: string;
  /** "https://lens.example.com" */
  origin: string;
  /** The global query of a shared link ("?from=…&to=…&system=…"), which every link carries. */
  global: string;
  /** The link to this A–B. */
  link: string;
}

const INDENT = "    ";

function utc(t: number): string {
  return `${isoSecond(t)} UTC`;
}

function stepLines(s: ReplayStep, fmt: (t: number) => string, url: (path: string) => string): string[] {
  const head = `${stepWhen(s, fmt)}  ${KIND_LABEL[s.kind]}${s.severity && s.severity !== "info" ? ` (${s.severity})` : ""}  ${s.title}`;
  const out = [head + (s.outsideRange ? "  [outside the range]" : "")];
  if (s.body) out.push(INDENT + s.body);
  const line = (e: ReplayEvidence, indent: string) => {
    const p = evidencePath(e);
    return `${indent}${evidenceWord(e)}: ${e.label}${p ? ` ${url(p)}` : ""}`;
  };
  for (const item of groupEvidence(s.evidence, s.leadersTaken)) {
    if (item.kind === "one") {
      out.push(line(item.e, INDENT));
      continue;
    }
    out.push(`${INDENT}events: ${electionsText(item)}`);
    for (const e of item.items) out.push(line(e, INDENT + INDENT));
  }
  const more = s.evidenceCount - s.evidence.length;
  if (more > 0) out.push(`${INDENT}and ${more} more`);
  return out;
}

/** The replay as text: the header, the steps in order, after B, the counts, the gone assets and the gaps. */
export function replayLog(r: Replay, ctx: LogContext): string {
  const m = { a: r.a, b: r.b };
  const fmt = whenFormat(m);
  const url = (path: string) => ctx.origin + withGlobal(path, ctx.global);
  const inRange = r.steps.filter((s) => !s.outsideRange);
  const after = r.steps.filter((s) => s.outsideRange);
  const lines: string[] = [
    "nats-lens replay",
    `System: ${ctx.system || "default"}`,
    `A: ${utc(r.a)}`,
    `B: ${utc(r.b)}`,
    `Collection at A: ${r.epochAAt !== undefined ? `${utc(r.epochAAt)} (epoch ${r.epochA})` : "none"}`,
    `Collection at B: ${r.epochBAt !== undefined ? `${utc(r.epochBAt)} (epoch ${r.epochB})` : "none"}`,
    FOOTER,
    `Link: ${ctx.link}`,
    ...collectionNotes(r),
    "",
    "What happened, in order",
    "",
  ];
  if (!inRange.length) lines.push("Nothing that nats-lens observed.");
  for (const s of inRange) lines.push(...stepLines(s, fmt, url));
  lines.push(...stepNotes(r));
  if (after.length) {
    lines.push("", "After B", "");
    for (const s of after) lines.push(...stepLines(s, fmt, url));
  }
  lines.push("", "Between A and B", "");
  for (const t of countTiles(r.counts)) {
    if (t.value !== undefined) {
      lines.push(`${t.label}: ${t.value}${t.note ? ` (${t.note})` : ""}`);
      continue;
    }
    const u = r.unobserved.find((x) => x.count === t.key);
    const why = u && unobservedText(u, fmt);
    lines.push(`${t.label}: not observed${why ? ` (${why})` : ""}`);
  }
  lines.push("", "Gone between A and B", "");
  if (!r.gone.length) lines.push("None that nats-lens observed.");
  for (const g of r.gone) {
    const p = gonePath(g, r.a);
    lines.push(`${entityName(g.entity)} (${goneKind(g)})${p ? ` ${url(p)}` : ""}`);
    lines.push(`${INDENT}at A: ${serversText(g.serversAtA)}; at B: ${goneAtB(g)}`);
    lines.push(
      `${INDENT}delete event: ${g.deleteEvent ? `${g.deleteEvent.summary || g.deleteEvent.kind}${!g.deleteEvent.summary && g.deleteClient ? ` by ${g.deleteClient}` : ""} ${url(evidencePath({ kind: "event", id: g.deleteEvent.id, label: "" })!)}` : "none captured"}`,
    );
    if (g.lastReported !== undefined) lines.push(`${INDENT}last reported: ${utc(g.lastReported)}`);
    if (g.consumersWithIt > 0) lines.push(`${INDENT}durables with it: ${g.consumersWithIt}`);
  }
  if (r.goneTruncated) lines.push("More assets went than the 200 listed.");
  const gaps = r.steps.filter((s) => s.kind === "gap");
  lines.push("", "Gaps", "");
  if (!gaps.length) lines.push("None.");
  for (const s of gaps) lines.push(`${stepWhen(s, fmt)}  ${s.title}`);
  return lines.join("\n") + "\n";
}

/** "replay-example-global-20261019T091400Z-20261019T095000Z.txt" */
export function logFileName(system: string, r: Pick<Replay, "a" | "b">): string {
  const sys = (system || "default").replace(/[^A-Za-z0-9._-]+/g, "_");
  return `replay-${sys}-${compactMoment(r.a)}-${compactMoment(r.b)}.txt`;
}

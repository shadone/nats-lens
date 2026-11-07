// Entry points describe context around observations, never an inferred incident duration.
import type { Finding, Transition, Window } from "../api/model";
import { clampMoments, momentText, WINDOW_PAD_MS, windowMoments, writeMoments, type Moments } from "./replayUrl";
import { writeRange } from "./timeRange";

/** ASVS V2.2: accept only finite, ordered observation times before building a URL.
 * GetReplay remains the authority for retention and evidence availability. */
export function replayEntry(m: Moments, now: number, minGapMs: number) {
  if (![m.a, m.b, now].every(Number.isFinite) || m.a > m.b || m.a > now) return undefined;
  const fixed = clampMoments(m, now, minGapMs);
  return {
    to: `/replay?${writeMoments(writeRange(new URLSearchParams(), { kind: "fixed", from: fixed.a, to: fixed.b }), fixed)}`,
    text: `Replay context: ${momentText(fixed.a)} to ${momentText(fixed.b)}.`,
    notes: fixed.notes,
  };
}

export function windowReplayEntry(w: Window, now: number, minGapMs: number) {
  if (![w.start, w.end].every(Number.isFinite) || w.start > w.end || w.start > now) return undefined;
  return replayEntry(windowMoments(w, now), now, minGapMs);
}

/** GetFinding's history is newest 100 across all time, not scoped to the selected
 * range. Pick the newest recorded transition inside that range, regardless of
 * array order. The finding's lifetime/conditionSince is not an episode boundary. */
export function findingReplayEntry(f: Finding, transitions: Transition[], start: number, end: number, now: number, minGapMs: number) {
  const candidates = [
    ...transitions.map(t => ({ time: t.time, label: t.state === "resolved" ? "resolution" : "transition" })),
    { time: f.since, label: "first sighting" },
    { time: f.resolvedAt, label: "resolution" },
  ].filter((t): t is { time: number; label: string } => t.time !== undefined && Number.isFinite(t.time) && t.time >= start && t.time <= Math.min(end, now));
  const latest = candidates.sort((a, b) => b.time - a.time)[0];
  if (!latest) return undefined;
  const entry = replayEntry({ a: latest.time - WINDOW_PAD_MS, b: latest.time + WINDOW_PAD_MS }, now, minGapMs);
  return entry && { ...entry, observation: `Context around the latest recorded ${latest.label} in this range (${momentText(latest.time)}), not the finding's duration.` };
}

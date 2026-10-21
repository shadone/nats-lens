// How a mute reads where it shows: a plain mute as "Muted by", an
// acceptance as by design as "Accepted by" with its review date, and one
// that nats-lens ended with its reason.
import type { Mute } from "../api/model";
import { isoDate } from "./format";

/** Who nats-lens records as the author of an acceptance it ended. */
export const LENS_AUTHOR = "nats-lens";

/** Whether the mute is in force at now: not lifted and not expired. */
export function muteActive(m: Mute, now: number): boolean {
  return m.unmutedAt === undefined && (m.expiresAt ?? 0) > now;
}

/** A reason as a sentence: a full stop added unless it ends with one. */
export function sentence(text: string): string {
  const t = text.trim();
  return /[.!?]$/.test(t) ? t : `${t}.`;
}

/** "Accepted by ops@example.com" or "Muted by ops". */
export function muteBy(m: Mute): string {
  return `${m.accepted ? "Accepted" : "Muted"} by ${m.mutedBy || "unknown"}`;
}

/** "Review on 2027-04-07." for an acceptance; empty for a mute or without a date. */
export function reviewOn(m: Mute): string {
  return m.accepted && m.expiresAt !== undefined ? `Review on ${isoDate(m.expiresAt)}.` : "";
}

/** "Ended by nats-lens: growth changed from …" when nats-lens ended the acceptance; empty otherwise. */
export function endedByLens(m: Mute): string {
  return m.unmutedBy === LENS_AUTHOR && m.endReason ? `Ended by ${LENS_AUTHOR}: ${m.endReason}.` : "";
}

/** The retention the growth review needs: a week, a day and an hour (api.AcceptRetention). */
export const ACCEPT_RETENTION_MS = (8 * 24 + 1) * 3_600_000;

/** Why the review cannot run under a short retention; empty when retentionMs is unknown (0) or long enough. */
export function shortRetentionText(retentionMs: number): string {
  if (retentionMs <= 0 || retentionMs >= ACCEPT_RETENTION_MS) return "";
  return `This nats-lens keeps ${fmtRetention(retentionMs)} of history and the growth review needs 8 days and an hour, so no baseline is recorded and the acceptance does not end early; it ends at its review date.`;
}

function fmtRetention(ms: number): string {
  const h = ms / 3_600_000;
  return h >= 48 && h % 24 === 0 ? `${h / 24} days` : `${Math.round(h)} hours`;
}

/**
 * The growth an acceptance is compared with, in words: the recorded rate,
 * or why none is recorded yet; under a retention shorter than the review
 * needs (retentionMs, 0 when unknown), why it never will be.
 */
export function baselineText(m: Mute, fmt: (perDay: number) => string, retentionMs = 0): string {
  const what = m.entity?.kind === "stream" ? "stream" : m.entity?.kind === "server" ? "server" : "";
  if (!m.accepted || !what) return "";
  if (m.growthPerDay !== undefined) {
    // A stream's rate is one copy's, as the end reason's is; the Capacity page's /day counts every replica.
    const measure = what === "stream" ? " per copy (the Capacity page's /day counts every replica)" : "";
    const short = shortRetentionText(retentionMs);
    return `Growth at acceptance: ${fmt(m.growthPerDay)}/day${measure}, over the last 7 days; ${short ? short.replace("This", "this").replace("no baseline is recorded and ", "") : "nats-lens ends the acceptance early when it changes by more than half of it, or of 1 MiB/day when that is larger."}`;
  }
  const short = shortRetentionText(retentionMs);
  if (short) return short;
  return `No growth baseline is recorded yet: growth is recorded once the ${what} has 8 days of history.`;
}

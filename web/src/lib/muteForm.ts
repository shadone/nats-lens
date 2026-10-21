// Validation of the mute and accept forms: a reason, an expiry or review date
// in range, and a name when the server cannot tell who is asking.

export const EXPIRY_PRESETS: { id: string; label: string; ms: number }[] = [
  { id: "1h", label: "1 hour", ms: 3_600_000 },
  { id: "8h", label: "8 hours", ms: 8 * 3_600_000 },
  { id: "24h", label: "24 hours", ms: 24 * 3_600_000 },
  { id: "7d", label: "7 days", ms: 7 * 24 * 3_600_000 },
  { id: "30d", label: "30 days", ms: 30 * 24 * 3_600_000 },
];

/** Limits the server enforces. */
export const MAX_REASON = 1000;
export const MIN_AHEAD = 60_000;
export const MAX_AHEAD = 90 * 24 * 3_600_000;
export const MAX_NAME = 256;

export interface MuteForm {
  reason: string;
  /** A preset ID, or "custom" with customExpiry set. */
  expiry: string;
  /** datetime-local value, read as UTC. */
  customExpiry: string;
  name: string;
}

export interface MuteErrors {
  reason?: string;
  expiry?: string;
  name?: string;
}

/** Expiry as epoch ms; NaN when it cannot be read. */
export function expiryOf(f: MuteForm, now: number): number {
  if (f.expiry === "custom") return f.customExpiry ? Date.parse(`${f.customExpiry}:00Z`) : NaN;
  const p = EXPIRY_PRESETS.find((x) => x.id === f.expiry);
  return p ? now + p.ms : NaN;
}

export function validateMute(f: MuteForm, now: number, askName: boolean): MuteErrors {
  const e: MuteErrors = {};
  const reason = f.reason.trim();
  if (reason.length === 0) e.reason = "Give a reason; it is shown with the muted finding.";
  else if ([...reason].length > MAX_REASON) e.reason = `At most ${MAX_REASON} characters.`;
  const exp = expiryOf(f, now);
  if (!Number.isFinite(exp)) e.expiry = "Choose when the mute ends.";
  else if (exp < now + MIN_AHEAD) e.expiry = "The mute must end at least a minute from now.";
  else if (exp > now + MAX_AHEAD) e.expiry = "A mute lasts at most 90 days.";
  const name = f.name.trim();
  if (askName && name.length === 0) e.name = "Enter your name; it is recorded with the mute.";
  else if (askName && [...name].length > MAX_NAME) e.name = `At most ${MAX_NAME} characters.`;
  return e;
}

export function hasErrors(e: MuteErrors): boolean {
  return Object.keys(e).length > 0;
}

/** Review dates an acceptance offers; months are calendar months, a year 364 days (a day inside the server's 365, so a browser clock a little ahead is not refused). */
export const REVIEW_PRESETS: { id: string; label: string }[] = [
  { id: "3mo", label: "in 3 months" },
  { id: "6mo", label: "in 6 months" },
  { id: "1y", label: "in 1 year" },
];

/** The server's limits on a review date are a day and 365 days; the form allows a day less at the far end. */
export const MIN_REVIEW_AHEAD = 24 * 3_600_000;
export const MAX_REVIEW_AHEAD = 364 * 24 * 3_600_000;

export interface AcceptForm {
  reason: string;
  /** A preset ID, or "date" with reviewDate set. */
  review: string;
  /** date input value (YYYY-MM-DD), read as the end of that day in UTC. */
  reviewDate: string;
  name: string;
}

export interface AcceptErrors {
  reason?: string;
  review?: string;
  name?: string;
}

function addMonths(now: number, months: number): number {
  const d = new Date(now);
  const day = d.getUTCDate();
  d.setUTCDate(1);
  d.setUTCMonth(d.getUTCMonth() + months);
  const last = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0)).getUTCDate();
  d.setUTCDate(Math.min(day, last));
  return d.getTime();
}

/** The review date as epoch ms; NaN when it cannot be read. A chosen date means the end of that UTC day. */
export function reviewDateOf(f: AcceptForm, now: number): number {
  switch (f.review) {
    case "3mo":
      return addMonths(now, 3);
    case "6mo":
      return addMonths(now, 6);
    case "1y":
      return now + MAX_REVIEW_AHEAD;
    case "date":
      return /^\d{4}-\d{2}-\d{2}$/.test(f.reviewDate) ? Date.parse(`${f.reviewDate}T23:59:00Z`) : NaN;
    default:
      return NaN;
  }
}

export function validateAccept(f: AcceptForm, now: number, askName: boolean): AcceptErrors {
  const e: AcceptErrors = {};
  const reason = f.reason.trim();
  if (reason.length === 0) e.reason = "Give a reason; it is shown with the accepted finding.";
  else if ([...reason].length > MAX_REASON) e.reason = `At most ${MAX_REASON} characters.`;
  const at = reviewDateOf(f, now);
  if (!Number.isFinite(at)) e.review = "Choose a review date.";
  else if (at < now + MIN_REVIEW_AHEAD) e.review = "The review date must be at least a day from now.";
  else if (at > now + MAX_REVIEW_AHEAD) e.review = "The review date is at most 364 days from now.";
  const name = f.name.trim();
  if (askName && name.length === 0) e.name = "Enter your name; it is recorded with the acceptance.";
  else if (askName && [...name].length > MAX_NAME) e.name = `At most ${MAX_NAME} characters.`;
  return e;
}

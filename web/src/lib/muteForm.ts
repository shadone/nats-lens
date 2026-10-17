// Validation of the mute form: a reason, an expiry in the future, and a name
// when the server cannot tell who is asking.

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

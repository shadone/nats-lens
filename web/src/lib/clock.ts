// The UI's now: the browser's clock moved to the server's. A live range
// leaves its end to the server, so its start must come from the same
// clock; `natslens demo -fixed` serves a moment weeks before the browser's,
// and its clock stands still there.

let offsetMs = 0;
let stoppedAt: number | undefined;

/** Now on the server's clock, in milliseconds since the epoch. */
export function serverNow(): number {
  return stoppedAt ?? Date.now() + offsetMs;
}

/**
 * Sets the clock from the server's now, as GetLensInfo reports it at
 * receivedAt. A stopped clock stays at that now.
 */
export function setServerNow(serverMs: number, receivedAt = Date.now(), stopped = false): void {
  offsetMs = serverMs - receivedAt;
  stoppedAt = stopped ? serverMs : undefined;
}

import type {ClientLaneBucket} from '../api/model';

/** The client events of adjacent buckets drawn as one mark. */
export interface ClientCell {
  key: string;
  from: number;
  to: number;
  buckets: number;
  connects: number;
  closes: number;
  drops: number;
  shutdowns: number;
  authErrors: number;
}

/** The kinds the client lane counts; the Events page filters by them. */
export const CLIENT_LANE_KINDS = ['client.connect', 'client.disconnect', 'client.churn', 'client.auth_error'];

export const cellTotal = (c: ClientCell) => c.connects + c.closes + c.drops + c.shutdowns + c.authErrors;
/** Drops and failed authentications: the disconnects a client did not ask for. */
export const cellProblems = (c: ClientCell) => c.drops + c.authErrors;

/**
 * Adjacent buckets merge into cells at least minPx wide at the track's
 * width, so each mark keeps a usable hit area; a cell without events is
 * left out. Without a measured width every bucket is a cell. Bounds stop
 * at the range's end.
 */
export function clientCells(buckets: ClientLaneBucket[], start: number, bucketMs: number, bucketCount: number, end: number, width: number, minPx = 12): ClientCell[] {
  if (!(bucketMs > 0) || bucketCount <= 0) return [];
  const per = width > 0 ? Math.max(1, Math.ceil((minPx * bucketCount) / width)) : 1;
  const cells = new Map<number, ClientCell>();
  for (const b of buckets) {
    if (b.index < 0 || b.index >= bucketCount) continue;
    const i = Math.floor(b.index / per);
    let c = cells.get(i);
    if (!c) {
      const first = i * per, last = Math.min(bucketCount, first + per);
      c = {key: `${first}`, from: start + first * bucketMs, to: Math.min(end, start + last * bucketMs), buckets: last - first,
        connects: 0, closes: 0, drops: 0, shutdowns: 0, authErrors: 0};
      cells.set(i, c);
    }
    c.connects += b.connects; c.closes += b.closes; c.drops += b.drops; c.shutdowns += b.shutdowns; c.authErrors += b.authErrors;
  }
  return [...cells.entries()].sort((a, b) => a[0] - b[0]).map(([, c]) => c).filter((c) => cellTotal(c) > 0);
}

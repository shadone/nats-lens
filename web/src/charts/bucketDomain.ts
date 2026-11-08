/** Buckets summarize reports filtered to [start, end), not a sample at the
 * bucket origin. Draw at the midpoint of the bucket's overlap with that
 * range. This is a display coordinate, never an observation timestamp. */
export function bucketPlotTime(time: number, start: number, end: number, stepMs: number): number {
  return (Math.max(start, time) + Math.min(end, time + stepMs)) / 2;
}

/** Exclude buckets touching only an exclusive boundary; never clamp an
 * unrelated out-of-range value into the plot. Keep original bucket times. */
export function overlappingBuckets<T extends { time: number }>(points: T[], start: number, end: number, stepMs: number): T[] {
  return points.filter(p => p.time < end && p.time + stepMs > start);
}

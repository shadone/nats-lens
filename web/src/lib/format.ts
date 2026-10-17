// Display formatting. Every time is shown in UTC, as in the reference.

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

export function num(n: number | bigint): number {
  return typeof n === "bigint" ? Number(n) : n;
}

/** 1,240 */
export function fmtInt(n: number | bigint): string {
  return Math.round(num(n)).toLocaleString("en-US");
}

/** A count that may be an average: 2.7, 13, 1,240. */
export function fmtCount(n: number): string {
  if (Math.abs(n) >= 100 || Number.isInteger(n)) return fmtInt(n);
  return trimZeros(n.toFixed(1));
}

function trimZeros(s: string): string {
  return s.includes(".") ? s.replace(/\.?0+$/, "") : s;
}

const BYTE_UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];

/** Bytes as [number, unit], three significant digits: ["61.2", "MiB"]. */
export function splitBytes(n: number | bigint): [string, string] {
  let v = Math.abs(num(n));
  let u = 0;
  while (v >= 1024 && u < BYTE_UNITS.length - 1) {
    v /= 1024;
    u++;
  }
  const digits = u === 0 || v >= 100 ? 0 : v >= 10 ? 1 : 2;
  const sign = num(n) < 0 ? "-" : "";
  return [sign + trimZeros(v.toFixed(digits)), BYTE_UNITS[u]];
}

/** 61.2 MiB */
export function fmtBytes(n: number | bigint): string {
  const [v, u] = splitBytes(n);
  return `${v} ${u}`;
}

/** +2.1 MiB, for growth per day. */
export function fmtGrowth(n: number): string {
  return (n >= 0 ? "+" : "") + fmtBytes(n);
}

/** Two units at most: 3d, 4h 59m, 12m, 45s. */
export function fmtDuration(ms: number): string {
  const s = Math.max(0, Math.round(ms / 1000));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return h > 0 && d < 7 ? `${d}d ${h}h` : `${d}d`;
  if (h > 0) return m > 0 ? `${h}h ${m}m` : `${h}h`;
  if (m > 0) return `${m}m`;
  return `${s}s`;
}

/** Round trip times and other short durations: 0.3 ms, 12 ms, 1.2 s. */
export function fmtShortDuration(ms: number): string {
  if (ms < 1) return `${trimZeros(ms.toFixed(2))} ms`;
  if (ms < 1000) return `${trimZeros(ms.toFixed(ms < 10 ? 1 : 0))} ms`;
  return `${trimZeros((ms / 1000).toFixed(1))} s`;
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** 14:30 */
export function hm(ms: number): string {
  const d = new Date(ms);
  return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}`;
}

/** 14:30:07 */
export function hms(ms: number): string {
  const d = new Date(ms);
  return `${hm(ms)}:${pad(d.getUTCSeconds())}`;
}

/** 17 Oct */
export function dayMon(ms: number): string {
  const d = new Date(ms);
  return `${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
}

/** 17 Oct 14:30 */
export function dayMonHm(ms: number): string {
  return `${dayMon(ms)} ${hm(ms)}`;
}

/** 2026-10-24 12:00 */
export function isoMinute(ms: number): string {
  const d = new Date(ms);
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${hm(ms)}`;
}

/** 2026-10-24 12:00:05 */
export function isoSecond(ms: number): string {
  return `${isoMinute(ms)}:${pad(new Date(ms).getUTCSeconds())}`;
}

export function sameUtcDay(a: number, b: number): boolean {
  return new Date(a).toISOString().slice(0, 10) === new Date(b).toISOString().slice(0, 10);
}

const DAY = 86_400_000;

/** A time within a day before ref as 09:31, an older one as 14 Oct. */
export function shortWhen(ms: number, ref: number): string {
  return Math.abs(ref - ms) < DAY ? hm(ms) : dayMon(ms);
}

/** An event time in a list: 09:31:07 within a day before ref, 16 Oct 09:31 otherwise. */
export function eventWhen(ms: number, ref: number): string {
  return Math.abs(ref - ms) < DAY ? hms(ms) : dayMonHm(ms);
}

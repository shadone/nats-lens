// Accessible names for charts: what is drawn, its latest value and the time
// it covers, so a screen reader hears what the eye reads from the chart.
import { dayMonHm } from "../lib/format";

/** "16 Oct 05:08 to 17 Oct 05:08 UTC" */
export function rangeText(start: number, end: number): string {
  return `${dayMonHm(start)} to ${dayMonHm(end)} UTC`;
}

/** "Messages, current 1,204, 16 Oct 05:08 to 17 Oct 05:08 UTC, as of 14:30:05 UTC" */
export function chartName(label: string, current: string | undefined, period: string, note?: string): string {
  const now = current === undefined ? "no data" : `current ${current}`;
  return [label, now, period, note].filter(Boolean).join(", ");
}

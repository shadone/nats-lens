// What the other pages read for their history notes. useChartGaps gives
// the hatched gaps a page's charts draw: every call reads the same
// GetDataHistory query, so a page view makes one request, and a page
// without charts makes none. useGrowthWait reads GetLensInfo and the
// shell's status, which the shell already reads, so it adds no request.
import { useMemo } from "react";
import { serverNow } from "../lib/clock";
import { growthWaitText, type ChartGaps } from "../lib/historyNotes";
import type { RangeSpec } from "../lib/timeRange";
import { useResolvedRange } from "../lib/useTimeRange";
import { isHatched } from "./history";
import { useDataHistory, useLensInfo, useShellStatus } from "./queries";

/**
 * The range's hatched gaps; undefined until the answer for this range
 * arrives, so another range's gaps are never drawn while it loads.
 */
export function useChartGaps(spec: RangeSpec): ChartGaps | undefined {
  const q = useDataHistory(spec);
  const d = q.isPlaceholderData ? undefined : q.data;
  const live = spec.kind === "live";
  return useMemo(() => (d ? { gaps: d.gaps.filter(isHatched), rangeEnd: d.rangeEnd, live } : undefined), [d, live]);
}

/** "needs 6 h of history; 5 h 29 min to go" while the range holds too little history for a growth rate; else undefined. */
export function useGrowthWait(spec: RangeSpec): string | undefined {
  const { start, end } = useResolvedRange(spec);
  const historyStart = useLensInfo().data?.historyStart;
  // Passive: the header follows the status; a second observer would ask again on mount.
  const f = useShellStatus({ passive: true }).data?.freshness;
  const stoppedAt = f && !f.collecting ? f.notCollectingSince : undefined;
  return growthWaitText({ historyStart, start, end, live: spec.kind === "live", now: serverNow(), stoppedAt, lastDataAt: stoppedAt === undefined ? undefined : f?.epochAt });
}

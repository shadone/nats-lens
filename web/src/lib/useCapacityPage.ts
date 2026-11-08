import { useState } from "react";
import { rangeKey } from "./timeRange";
import { useSystem } from "./useSystem";
import { useTimeRange } from "./useTimeRange";

/** Normalize stored state too: a later growth/filter round trip must not
 * resurrect an invisible page. Scope changes start a new investigation. */
export function useCapacityPage(count: number, size: number, filter = "") {
  const system = useSystem();
  const { spec } = useTimeRange();
  const context = JSON.stringify([system, rangeKey(spec), filter]);
  const [stored, setStored] = useState({ context, page: 0 });
  const page = stored.context === context ? Math.min(stored.page, Math.max(0, Math.ceil(count / size) - 1)) : 0;
  if (stored.context !== context || stored.page !== page) setStored({ context, page });
  return [page, (next: number) => setStored({ context, page: next })] as const;
}

// The global time range from the URL, with setters that keep other
// parameters, and a clock for live ranges. Links take the range from
// useGlobalQuery, with the system and the account.
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { parseRange, rangeProblem, resolveRange, writeRange, type RangeSpec } from "./timeRange";

export function useTimeRange() {
  const [params, setParams] = useSearchParams();
  const key = ["range", "from", "to"].map((k) => params.get(k) ?? "").join("|");
  const spec = useMemo(() => parseRange(params), [key]);
  const problem = useMemo(() => rangeProblem(params), [key]);
  /**
   * Presets, live and pause replace the history entry; a custom range is
   * a deliberate choice and gets its own entry, so Back returns to it.
   */
  const setSpec = useCallback(
    (next: RangeSpec, opts: { push?: boolean } = {}) => setParams((p) => writeRange(p, next), { replace: !opts.push }),
    [setParams],
  );
  /** Removes the range from the link, keeping other parameters. */
  const clearRange = useCallback(
    () =>
      setParams(
        (p) => {
          const n = new URLSearchParams(p);
          ["range", "from", "to"].forEach((k) => n.delete(k));
          return n;
        },
        { replace: true },
      ),
    [setParams],
  );
  return { spec, setSpec, clearRange, problem };
}

/** Now, ticking every interval while enabled. */
export function useNow(intervalMs = 1000, enabled = true): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!enabled) return;
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs, enabled]);
  return now;
}

/** Start and end of the current range; a live range moves with the clock. */
export function useResolvedRange(spec: RangeSpec) {
  const now = useNow(30_000, spec.kind === "live");
  return useMemo(() => resolveRange(spec, now), [spec, now]);
}

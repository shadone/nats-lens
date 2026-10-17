// One banner per page for queries whose last refresh failed while they
// still hold data. Each query reports itself; the provider shows one banner
// with the newest time any of them last succeeded, and one Retry for all.
import { createContext, useCallback, useContext, useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { errorInfo } from "../api/errors";
import { hms } from "../lib/format";

export interface StaleEntry {
  at?: number;
  error: unknown;
  refetch: () => unknown;
}

type Report = (id: string, entry: StaleEntry | undefined) => void;

const StaleCtx = createContext<Report | null>(null);

export function StaleBannerView({ entries }: { entries: StaleEntry[] }) {
  if (!entries.length) return null;
  const times = entries.map((e) => e.at).filter((t): t is number => t !== undefined);
  const newest = times.length ? Math.max(...times) : undefined;
  const infos = entries.map((e) => errorInfo(e.error));
  const codes = [...new Set(infos.map((i) => (i.code === "error" ? i.message : i.code)))];
  const hint = infos.find((i) => i.hint)?.hint;
  return (
    <div className="state-box stale" role="status">
      <span>
        Showing data from {newest !== undefined ? `${hms(newest)} UTC` : "an earlier refresh"}; the last refresh failed: {codes.join(", ")}.
        {hint && <span className="ehint"> {hint}</span>}
      </span>
      <button type="button" className="btn" onClick={() => entries.forEach((e) => e.refetch())}>
        Retry
      </button>
    </div>
  );
}

/** Holds the failing queries of one page and shows their banner first. */
export function StaleProvider({ children }: { children: ReactNode }) {
  const [entries, setEntries] = useState<Record<string, StaleEntry>>({});
  const report = useCallback<Report>((id, entry) => {
    setEntries((prev) => {
      if (entry) return { ...prev, [id]: entry };
      if (!(id in prev)) return prev;
      const next = { ...prev };
      delete next[id];
      return next;
    });
  }, []);
  const list = useMemo(() => Object.values(entries), [entries]);
  return (
    <StaleCtx.Provider value={report}>
      <StaleBannerView entries={list} />
      {children}
    </StaleCtx.Provider>
  );
}

/**
 * Reports a query to the page's banner while it holds data and its last
 * refresh failed. Returns false when there is no provider, so the caller
 * shows a banner of its own.
 */
export function useReportStale(q: { data: unknown; error: unknown; dataUpdatedAt?: number; refetch: () => unknown }): boolean {
  const report = useContext(StaleCtx);
  const id = useId();
  const stale = q.data !== undefined && !!q.error;
  const { error, dataUpdatedAt, refetch } = q;
  useEffect(() => {
    if (!report) return;
    report(id, stale ? { at: dataUpdatedAt, error, refetch } : undefined);
  }, [report, id, stale, error, dataUpdatedAt, refetch]);
  useEffect(() => () => report?.(id, undefined), [report, id]);
  return report !== null;
}

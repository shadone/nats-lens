// History facts shared by a page. LensInfo uses the shell's existing cache;
// only a mounted chart subscribes to the range's shared Data history query.
import { createContext, useCallback, useContext, useEffect, useState, useSyncExternalStore, type ReactNode } from "react";
import { notifyManager, useQueryClient } from "@tanstack/react-query";
import { useReportStale } from "./StaleContext";
import { Link, useLocation } from "react-router";
import type { HistoryGap, ServerHistoryGap } from "../api/history";
import { dataHistoryPrefix, isServerHistoryKey, useDataHistory, useLensInfo } from "../api/queries";
import { growthHistoryNote, historyNote } from "../lib/historyView";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import { useSystem } from "../lib/useSystem";

export interface PageHistory {
  historyStart?: number;
  intervalMs?: number;
  start: number;
  end: number;
  live: boolean;
  gaps?: HistoryGap[];
  serverGaps?: ServerHistoryGap[];
  gapsStatus?: string;
  gapsTotal?: number;
  newestOmittedCollectionGap?: HistoryGap;
  gapServer?: string;
}
export const HistoryContext = createContext<PageHistory | undefined>(undefined);
export const usePageHistory = () => useContext(HistoryContext);

export function PageHistoryProvider({ children }: { children: ReactNode }) {
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const info = useLensInfo().data;
  const { pathname } = useLocation();
  const readsRange = !["/fleet", "/data-history"].includes(pathname);
  return <HistoryContext.Provider value={{ start, end, live: spec.kind === "live", historyStart: info?.historyStart, intervalMs: info?.collectIntervalMs }}>
    {readsRange && <HistoryNote />}
    {readsRange && <PageHistoryStatus />}
    {children}
  </HistoryContext.Provider>;
}

/** The state of the history reads the page's charts make, once for the page:
 * each chart shows it as plain text, and one live region per chart would say
 * it many times. Server-scoped reads are left to ServerChartHistory's own.
 * Loading is said only once a read has been pending LOADING_DELAY_MS, so a
 * read the cache answers at once does not flash it; unavailable is said at once,
 * with a visible note, as the charts keep their rows and do not say it. */
const LOADING = "History gaps loading";
const UNAVAILABLE = "History gaps unavailable";
const LOADING_DELAY_MS = 300;
function PageHistoryStatus() {
  const cache = useQueryClient().getQueryCache();
  const sys = useSystem();
  const subscribe = useCallback((changed: () => void) => cache.subscribe(notifyManager.batchCalls(changed)), [cache]);
  const now = useSyncExternalStore(subscribe, () => {
    let pending = false;
    for (const q of cache.findAll({ queryKey: dataHistoryPrefix(sys) })) {
      if (q.getObserversCount() === 0 || isServerHistoryKey(q.queryKey)) continue;
      if (q.state.status === "error") return UNAVAILABLE;
      if (q.state.status === "pending") pending = true;
    }
    return pending ? LOADING : "";
  });
  const [text, setText] = useState("");
  useEffect(() => {
    if (now !== LOADING) {
      setText(now);
      return;
    }
    // A new read makes an earlier "unavailable" stale at once.
    setText((t) => (t === LOADING ? t : ""));
    const timer = setTimeout(() => setText(LOADING), LOADING_DELAY_MS);
    return () => clearTimeout(timer);
  }, [now]);
  return <>
    {now === UNAVAILABLE && <p className="dh-note">History gaps unavailable: gaps are not marked on these charts.</p>}
    <p className="sr-only" role="status">{text}</p>
  </>;
}

export function HistoryNote() {
  const h = usePageHistory();
  const text = h && historyNote(h.historyStart, h.start, h.end, h.intervalMs ?? 0, h.live);
  // ASVS 1.2.1/1.2.2: React encodes text and RLink carries encoded global query parameters.
  return text ? <p className="dh-note" role="note">{text} <HistoryLink /></p> : null;
}

export function useGrowthHistoryNote() {
  const h = usePageHistory();
  return h ? growthHistoryNote(h.historyStart, h.start, h.end, h.live) : undefined;
}

export function HistoryLink({ children }: { children?: ReactNode }) {
  const query = useGlobalQuery();
  return <Link className="x" to={`/data-history${query}`}>{children ?? "Data history"}</Link>;
}

/** One history read shared by every metric on this server, scoped by system,
 * exact range key and server name. No previous-range/server placeholders. */
export function ServerChartHistory({ server, children }: { server: string; children: ReactNode }) {
  const h = usePageHistory();
  const { spec } = useTimeRange();
  const q = useDataHistory(spec, server);
  useReportStale(q);
  const d = q.data;
  const status = q.error ? "History gaps unavailable" : q.isPending ? "History gaps loading" : undefined;
  return <HistoryContext.Provider value={h && { ...h, start: d?.rangeStart ?? h.start, end: d?.rangeEnd ?? h.end, gaps: d?.gaps ?? [], serverGaps: d?.serverGaps ?? [], gapsTotal: d?.gapsTotal, newestOmittedCollectionGap: d?.newestOmittedCollectionGap, gapServer: server, gapsStatus: status }}>
    {status && <p className="dh-note" role="status">{status}</p>}
    <p className="dh-note">Server hatches mark recorded missing answers or missing STATSZ/VARZ metrics reports, at collection intervals. They do not establish server downtime.</p>
    {children}
  </HistoryContext.Provider>;
}

// History facts shared by a page. LensInfo uses the shell's existing cache;
// only a mounted chart subscribes to the range's shared Data history query.
import { createContext, useContext, type ReactNode } from "react";
import { useReportStale } from "./StaleContext";
import { Link, useLocation } from "react-router";
import type { HistoryGap, ServerHistoryGap } from "../api/history";
import { useDataHistory, useLensInfo } from "../api/queries";
import { growthHistoryNote, historyNote } from "../lib/historyView";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { useGlobalQuery } from "../lib/useGlobalQuery";

export interface PageHistory {
  historyStart?: number;
  intervalMs?: number;
  start: number;
  end: number;
  live: boolean;
  gaps?: HistoryGap[];
  serverGaps?: ServerHistoryGap[];
  gapsStatus?: string;
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
    {children}
  </HistoryContext.Provider>;
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
  return <HistoryContext.Provider value={h && { ...h, start: d?.rangeStart ?? h.start, end: d?.rangeEnd ?? h.end, gaps: d?.gaps ?? [], serverGaps: d?.serverGaps ?? [], gapsStatus: status }}>
    {status && <p className="dh-note" role="status">{status}</p>}
    <p className="dh-note">Server hatches mark recorded missing answers or missing STATSZ/VARZ metrics reports, at collection intervals. They do not establish server downtime.</p>
    {children}
  </HistoryContext.Provider>;
}

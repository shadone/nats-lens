import { useQuery } from "@tanstack/react-query";
import type { CapacityStream } from "../api/capacity";
import type { Entity } from "../api/model";
import { getTrends, pickTrend, type TrendQuery, type TrendSlice } from "../api/trendBatch";
import { LIVE_REFRESH_MS, rangeKey } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { fmtBytes } from "../lib/format";
import { Sparkline } from "../charts/Sparkline";
import { OneLine } from "./Trends";
import { errorText } from "./common";
import { useReportStale } from "./StaleContext";

export function serverHistoryQuery(name: string): TrendQuery {
 const entity: Entity = {kind:"server", key:name, name, server:name, account:"", stream:"", consumer:""};
 return {metric:"server.capacity_file_store_bytes",entity};
}
export function resourceHistoryQuery(s: CapacityStream, replicas = false): TrendQuery {
 const entity: Entity = {kind:"stream",key:`${s.account}/${s.stream}`,name:s.stream,server:"",account:s.account,stream:s.stream,consumer:""};
 return {metric: replicas ? "stream.observed_replica_bytes" : "stream.bytes",entity};
}
/** Callers supply at most 32 series from the visible page; no per-row reads.
 * No placeholder crosses a range or row-page change. React Query cancels the
 * request when its final observer leaves, forwarding the signal to Connect. */
export function useCapacityHistory(queries: TrendQuery[]) {
 const {spec} = useTimeRange(); const system = useSystem();
 const q = useQuery({queryKey:["capacityHistory",system,rangeKey(spec),queries],queryFn:({signal}) => {
  if (queries.length > 32) throw new Error("Capacity history batch exceeds 32 series");
  return getTrends(system,spec,queries,signal);
 },enabled:queries.length>0,refetchInterval:spec.kind === "live" ? LIVE_REFRESH_MS : false});
 useReportStale(q);
 return (want:TrendQuery) => pickTrend(q,queries,want);
}
export function CapacityHistoryChart({q,title,compact=false}: {q:TrendSlice;title:string;compact?:boolean}) {
 const {spec} = useTimeRange(); const {start,end} = useResolvedRange(spec);
 if (!compact) return <OneLine f={{spec,start,end}} q={q} title={title} label={title} />;
 return <div className="cap-history-compact">
  {q.error && !q.data ? <small>History unavailable: {errorText(q.error)}</small> : <Sparkline points={q.data?.points ?? []} stepMs={q.data?.stepMs} fmt={fmtBytes} bucketNote="Mean logical bytes in bucket; missing observations are not zero" status="ok" tone="change" windows={[]} start={start} end={end} label={title} />}
 </div>;
}
export function TotalCapacityHistory() {
 const query = {metric:"cluster.capacity_file_store_bytes"}; const get = useCapacityHistory([query]);
 return <section aria-label="File-store usage history" className="cap-history-total">
  <CapacityHistoryChart q={get(query)} title="Total JetStream file-store usage" />
  <p className="cap-note">Server file-store usage includes observed file replicas, excludes memory resources, and is independent of resource filters. Missing expected servers, missing reports, and ambiguous server names leave totals unknown. Buckets show means; collection and retention gaps are marked.</p>
 </section>;
}

import { fromJson, toJson } from "@bufbuild/protobuf";
import { TimestampSchema, type Timestamp } from "@bufbuild/protobuf/wkt";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { lens } from "../api/client";
import type { StorageChangeBytes, StorageChangeRow, GetStorageChangeResponse } from "../gen/natslens/v1/storage_change_pb";
import { QueryView, Section } from "../components/common";
import {useTimeRange} from "../lib/useTimeRange";
import {resolveRange} from "../lib/timeRange";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { serverPath, streamPath } from "../lib/view";

const stamp = (t: Timestamp | undefined) => t ? String(toJson(TimestampSchema, t)) : "Unknown";
const bytes = (v: bigint | undefined, signed = false) => v === undefined ? "Unknown" : `${v < 0n ? "−" : signed && v > 0n ? "+" : ""}${(v < 0n ? -v : v).toLocaleString("en-US")} bytes`;
const reasons: Record<string, string> = {
  no_comparable_endpoints:"Separate fresh collection windows are required to measure a change; retained values alone do not establish change in this interval",
  resource_inventory_incomplete:"Detailed resource reports do not match the server-reported inventory; totals are unknown",
 exclusive_end_inclusion:"The captured source uses its disclosed one-microsecond exclusive end; observations stop at the original source clock",offline_ui_unavailable:"No remote UI serves this local dataset",
 no_collection: "No retained collection at or before this moment", stale_collection: "The selected collection is older than three collection intervals",
  missing_jsz: "Missing JSZ evidence; complete storage totals are unavailable", server_usage_is_system_wide: "Server usage covers all accounts; it cannot be reconciled with this account's resources",
  observed_delete_event: "A recorded deletion qualifies absence; it does not establish the cause of other changes", incarnation_replaced: "A later creation time establishes a replaced incarnation",
  not_observed_at_a: "Not observed at A; absence is not zero", not_observed_at_b: "Not observed at B; absence is not zero", incarnation_unknown: "Creation identity is unknown; endpoint values are not a comparable incarnation",
  invalid_creation_time: "Reported creation time exceeds its collection window", created_after_a: "Reported creation time is after A", configured_replicas_changed: "Configured replicas differ between the endpoints",
  replica_coverage_incomplete: "Not every configured copy has a usable report", ambiguous_configuration_or_identity: "Configuration or resource identity is ambiguous", leader_or_copy_ambiguity: "Leader or copy reports disagree",
  no_leader_consistent_copies: "No leader report; logical size uses agreeing copy reports", server_measurement_unavailable: "Server measurement is unavailable", server_process_changed: "A different server process reports this name at B",
};
function reason(x: string) { return reasons[x] ?? x.replaceAll("_", " "); }
function pin(raw: string | null): bigint {
  if (raw === null) return 0n;
  if (!/^[1-9]\d{0,18}$/.test(raw) || BigInt(raw) > 9223372036854775807n) throw new Error("The collection pin is invalid.");
  return BigInt(raw);
}
function Values({ value }: { value: StorageChangeBytes | undefined }) {
  return <dl className="kv"><dt>Logical</dt><dd>{bytes(value?.logical)}</dd><dt>Observed copies</dt><dd>{bytes(value?.observedReplicas)}</dd><dt>Configured-replica estimate</dt><dd>{bytes(value?.estimatedReplicas)}</dd><dt>Server-reported usage</dt><dd>{bytes(value?.serverReported)}</dd></dl>;
}
export function StorageChange(){
 usePageTitle("Storage change");const [params]=useSearchParams();
 if(!params.has("from")&&!params.has("to"))return <ResolveComparisonRange/>;
 return <PinnedStorageChange/>;
}
function ResolveComparisonRange(){
 const system=useSystem();const info=useQuery({queryKey:["storage-change-clock",system],queryFn:async({signal})=>{const r=await lens.getLensInfo({system},{signal});if(!r.now)throw new Error("The source clock is unavailable.");return Number(r.now.seconds)*1000+r.now.nanos/1e6;},staleTime:0});const {spec,problem}=useTimeRange();const [params,setParams]=useSearchParams();
 const key=params.toString();
 useEffect(()=>{if(info.data===undefined||problem)return;const r=resolveRange(spec,info.data);setParams(p=>{const n=new URLSearchParams(p);n.delete("range");n.delete("epochA");n.delete("epochB");n.set("from",new Date(r.start).toISOString());n.set("to",new Date(r.end).toISOString());return n},{replace:true})},[info.data,key,problem,setParams]);
 return <><h1>Storage change</h1>{problem?<p role="alert">{problem}</p>:<QueryView q={info} what="source clock">{()=> <p role="status">Freezing the selected interval…</p>}</QueryView>}</>;
}
function PinnedStorageChange() {
  const system = useSystem();
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const account = params.get("account") ?? "", from = params.get("from") ?? "", to = params.get("to") ?? "";
  const epochA = params.get("epochA"), epochB = params.get("epochB");
  const scope = [system, account, from, to, epochA, epochB].join("|");
  const [paging, setPaging] = useState({ scope, token: "", previous: [] as string[], offset: 0 });
  const page = paging.scope === scope ? paging : { scope, token: "", previous: [] as string[], offset: 0 };
  let problem: string | undefined;
  let request;
  try {
    const a = fromJson(TimestampSchema, from), b = fromJson(TimestampSchema, to);
    if (a.seconds > b.seconds || a.seconds === b.seconds && a.nanos >= b.nanos) throw new Error("The comparison needs an increasing interval.");
    request = { system, account, a, b, epochA: pin(epochA), epochB: pin(epochB), pageSize: 100, pageToken: page.token };
  } catch (e) { problem = e instanceof Error ? e.message : "The comparison link is invalid."; }
  const q = useQuery({ queryKey: ["storage-change", scope, page.token], enabled: !!request, queryFn: ({ signal }) => lens.getStorageChange(request!, { signal }), staleTime: Infinity });
  const data = q.data;
  const resolvedSystem=system||data?.system||"";
  const resolvedA=data?.a?.epoch ? String(data.a.epoch) : epochA;
  const resolvedB=data?.b?.epoch ? String(data.b.epoch) : epochB;
  const normalizing=!!data && (resolvedSystem!==system || resolvedA!==epochA || resolvedB!==epochB);
  useEffect(() => {
    if (!data || !normalizing) return;
    const pinnedScope = [resolvedSystem, account, from, to, resolvedA, resolvedB].join("|");
    queryClient.setQueryData(["storage-change", pinnedScope, ""], data);
    setParams(p => { const n = new URLSearchParams(p); if(resolvedSystem)n.set("system",resolvedSystem); if(resolvedA)n.set("epochA",resolvedA); if(resolvedB)n.set("epochB",resolvedB); return n; }, { replace: true });
  }, [data, normalizing, resolvedSystem, resolvedA, resolvedB, setParams, account, from, to, queryClient]);
  const path = (target: string, id?: string) => {
    const n = new URLSearchParams({ system, account, from, to }); if (id) n.set("id", id); return `${target}?${n}`;
  };
  return <>
    <div className="page-h"><h1>Storage change</h1><Link className="x" to={path("/capacity")}>Capacity</Link></div>
    <p>Compare retained observations, not a daily forecast. Logical bytes are one copy. Observed copy bytes are reported logical copies, not physical disk allocation. Configured-replica bytes are an estimate.</p>
    <p>Requested: {from || "Unknown"} to {to || "Unknown"}. {account ? `Resources in ${account}; server usage remains system-wide.` : "All accounts."} Configuration and events are observations, not proven causes.</p>
    {problem ? <p role="alert">{problem}</p> : <QueryView q={q} what="storage comparison">{d => <>
      <Section title="Pinned collection windows"><dl className="kv">
        <dt>A</dt><dd>Collection {String(d.a?.epoch ?? 0n)} · {stamp(d.a?.startedAt)} to {stamp(d.a?.finishedAt)}{d.a?.reasons.map(x => <p key={x}>{reason(x)}</p>)}</dd>
        <dt>B</dt><dd>Collection {String(d.b?.epoch ?? 0n)} · {stamp(d.b?.startedAt)} to {stamp(d.b?.finishedAt)}{d.b?.reasons.map(x => <p key={x}>{reason(x)}</p>)}</dd>
      </dl><p>Collections finish at or before the requested moments. Pins fail explicitly if their retained evidence is removed.</p></Section>
      {d.reasons.map(x => <p role="note" key={x}>{reason(x)}</p>)}
      {d.totals.map(t => <Section title={`${t.storage === "memory" ? "Memory" : "File"} storage change`} key={t.storage}>
        <dl className="kv"><dt>Complete logical change</dt><dd>{bytes(t.delta?.logical, true)}</dd><dt>Complete observed-copy change</dt><dd>{bytes(t.delta?.observedReplicas, true)}</dd><dt>Server usage change</dt><dd>{bytes(t.delta?.serverReported, true)}</dd><dt>Known resource contributions</dt><dd>{bytes(t.knownContributions?.logical, true)} logical · {bytes(t.knownContributions?.observedReplicas, true)} observed copies</dd><dt>Unknown resource rows</dt><dd>{t.unknownRows}</dd><dt>Outside this page</dt><dd>{bytes(t.omittedContributions?.logical, true)} logical</dd><dt>Unreconciled difference</dt><dd>{bytes(t.unreconciled, true)} · server usage minus observed-copy change; not physical accounting</dd></dl>
      </Section>)}
      <Section title="Contributors and evidence"><p>Showing {d.rows.length ? `${page.offset + 1}–${page.offset + d.rows.length}` : "0"} of {d.totalCount} rows</p><p>Resources rank by absolute logical change, including decreases; unknown contributions follow. Server stores follow resources.</p>
        <div className="table-wrap" role="region" aria-label="Storage contributors" tabIndex={0}><table><thead><tr><th>Resource or server</th><th>Logical change</th><th>Observed-copy change</th><th>Estimated-copy change</th><th>Server usage change</th><th>Evidence</th></tr></thead><tbody>{d.rows.map(r => <tr key={r.id}>
          <td><Link className="x" to={resourceHistoryPath(r,d,system,account,from,to)}>{r.bucket || r.stream || r.server}</Link><p>{r.storage} · {r.account || "All accounts"}</p></td>
          <td>{bytes(r.delta?.logical, true)}</td><td>{bytes(r.delta?.observedReplicas, true)}</td><td>{bytes(r.delta?.estimatedReplicas, true)}</td><td>{bytes(r.delta?.serverReported, true)}</td>
          <td><RowEvidence row={r} eventPath={id => path("/events", id)} /></td>
        </tr>)}</tbody></table></div>
        <div className="page-actions"><button type="button" disabled={!page.previous.length || q.isFetching || normalizing} onClick={() => { const previous = [...page.previous]; const token = previous.pop()!; setPaging({ scope, token, previous, offset: Math.max(0, page.offset - 100) }); }}>Previous contributors</button><button type="button" disabled={!d.nextPageToken || q.isFetching || normalizing} onClick={() => setPaging({ scope, token: d.nextPageToken, previous: [...page.previous, page.token], offset: page.offset + d.rows.length })}>Next contributors</button></div>
      </Section>
    </>}</QueryView>}
  </>;
}
function RowEvidence({ row, eventPath }: { row: StorageChangeRow; eventPath: (id: string) => string }) {
  return <details><summary>Before, after and evidence</summary><p>Creation: {stamp(row.created)}</p><h3>Before</h3><Values value={row.before}/><h3>After</h3><Values value={row.after}/><p>Processes at A: {row.processesA.join(", ") || "Unknown"}; at B: {row.processesB.join(", ") || "Unknown"}.</p><p>Configured copies: {row.configuredReplicasA ?? "Unknown"} → {row.configuredReplicasB ?? "Unknown"}.</p>{row.reasons.map(x => <p key={x}>{reason(x)}</p>)}{row.eventIds.map(id => <p key={String(id)}><Link className="x" to={eventPath(String(id))}>Event {String(id)}</Link></p>)}<p>Comparison pins identify the source windows. Resource links open the last observed endpoint window, with a whole-second exclusive upper-bound tick. Comparison pins remain authoritative; other detail reads do not have universal pins.</p></details>;
}

function resourceHistoryPath(row:StorageChangeRow,data:GetStorageChangeResponse,system:string,account:string,from:string,to:string){
 const endpoint=row.processesB.length||!row.processesA.length?data.b:data.a;
 if(endpoint?.startedAt&&endpoint.finishedAt){
  from=stamp(endpoint.startedAt);
  // Other detail links round URL ranges to whole seconds. Use their
  // smallest exclusive-end tick; exact source windows remain above.
  to=new Date((Number(endpoint.finishedAt.seconds)+1)*1000).toISOString().replace(".000Z","Z");
 }
 const q=new URLSearchParams({system,account:row.account||account,from,to});
 return `${row.rowKind==="server"?serverPath(row.server):streamPath(row.account,row.stream)}?${q}`;
}

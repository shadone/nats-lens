import {useState} from "react";
import {Link} from "react-router";
import {useStorageContributors} from "../api/useStorageContributors";
import type {Entity} from "../api/model";
import {pickTrend,useTrends} from "../api/trendBatch";
import {StorageChangeOrder,type GetStorageChangeResponse,type StorageChangeRow} from "../gen/natslens/v1/storage_change_pb";
import {bytes,stamp,reason,resourceEndpointPath} from "../lib/storageChangeView";
import {QueryView} from "./common";
import {InfoTip} from "./InfoTip";
import {OneLine} from "./Trends";
import "../styles/storage-contributors.css";

function comparisonParams(d:GetStorageChangeResponse){
 const p=new URLSearchParams({system:d.system,account:d.account,from:stamp(d.a?.requested),to:stamp(d.b?.requested),contributors:"1"});
 if(d.a?.epoch)p.set("epochA",String(d.a.epoch));if(d.b?.epoch)p.set("epochB",String(d.b.epoch));return p;
}
function magnitude(n:bigint){return n<0n?-n:n}
function ChangeBar({value,max}:{value:bigint|undefined;max:bigint}){
 const width=value!==undefined&&max>0n?Number(magnitude(value)*5000n/max)/100:0;
 return <span className="contributor-bar" aria-hidden="true"><span className={value!==undefined&&value<0n?"decrease":"increase"} style={{width:`${width}%`,left:`${value!==undefined&&value<0n?50-width:50}%`}}/></span>;
}
export function CapacityContributors(){
 const c=useStorageContributors();const [expanded,setExpanded]=useState({selection:"",id:""});
 return <section className="card cap-contributors" aria-label="Resources by measured change">
  <div className="page-h"><h2>Resources by measured change</h2><button type="button" className="btn" onClick={c.refresh} disabled={c.q.isFetching||!c.ready}>Refresh comparison</button></div>
  <p>Signed one-copy changes, including decreases and resources absent from the latest collection. Frozen until refreshed; independent of daily forecasts.</p>
  {c.problem?<p role="alert">{c.problem}</p>:!c.ready?<p role="status">Resolving source clock…</p>:<QueryView q={c.q} what="storage contributors">{d=>{
   const p=comparisonParams(d);if(c.order===StorageChangeOrder.LOGICAL_ASCENDING)p.set("change","asc");if(c.order===StorageChangeOrder.LOGICAL_DESCENDING)p.set("change","desc");if(c.type&&c.type!=="all")p.set("resource",c.type);
   const max=d.rows.reduce((m,r)=>r.delta?.logical!==undefined&&magnitude(r.delta.logical)>m?magnitude(r.delta.logical):m,0n);
   const selected=expanded.selection===c.selection?d.rows.find(r=>r.id===expanded.id):undefined;
   return <>
    <p><Link className="x" to={`/capacity/storage-change?${p}`}>Open pinned investigation</Link></p>
    <p className="mono">A: {stamp(d.a?.startedAt)} – {stamp(d.a?.finishedAt)} · B: {stamp(d.b?.startedAt)} – {stamp(d.b?.finishedAt)}</p>
    <dl className="contributor-headlines">{d.totals.map(t=><div key={t.storage}><dt>{t.storage==="memory"?"Memory":"File"} logical change</dt><dd>{bytes(t.delta?.logical,true)}</dd></div>)}</dl>
    <details><summary>Totals and coverage</summary>
    <p>Totals cover all resource types in this account scope. Server usage is system-wide. Averages, configured-copy estimates and physical allocation are different quantities.</p>
    <dl className="contributor-totals">{d.totals.map(t=><div key={t.storage}><dt>{t.storage==="memory"?"Memory":"File"} logical change</dt><dd>{bytes(t.delta?.logical,true)}</dd><dt>Known contributions</dt><dd>{bytes(t.knownContributions?.logical,true)} · {t.unknownRows} unknown rows</dd><dt>Unreconciled difference</dt><dd>{bytes(t.unreconciled,true)} · server usage minus complete observed-copy change</dd><dt>Outside this page or type filter</dt><dd>{bytes(t.omittedContributions?.logical,true)}</dd></div>)}</dl>
    {[...(d.a?.reasons??[]),...(d.b?.reasons??[]),...d.reasons].filter((x,i,a)=>a.indexOf(x)===i).map(x=><p role="note" key={x}>{reason(x)}</p>)}
    </details>
    <label>Contributor type <select value={c.type==="stream"||c.type==="kv"||c.type==="object"?c.type:"all"} onChange={e=>c.setParams(old=>{const next=new URLSearchParams(old);if(e.target.value==="all")next.delete("resource");else next.set("resource",e.target.value);return next})}><option value="all">All resources</option><option value="stream">Streams</option><option value="kv">KV buckets</option><option value="object">Object stores</option></select></label>
    <p>Showing {d.rows.length?`${c.page.offset+1}–${c.page.offset+d.rows.length}`:"0"} of {d.totalCount} matching resource incarnations. Sorting applies before paging. Unknown changes remain last.</p>
    <div className="table-wrap" tabIndex={0} role="region" aria-label="Measured contributors"><table className="tbl"><thead><tr><th>Resource</th><th aria-sort={c.order===StorageChangeOrder.LOGICAL_ASCENDING?"ascending":c.order===StorageChangeOrder.LOGICAL_DESCENDING?"descending":"none"}><button type="button" aria-label="Sort by Change in range" onClick={()=>c.setParams(old=>{const next=new URLSearchParams(old);next.set("change",c.order===StorageChangeOrder.LOGICAL_DESCENDING?"asc":"desc");return next})}>Change in range</button><InfoTip label="Change in range" text="One-copy logical bytes at B minus A, with the actual collection windows above. Distinct fresh observations and known identity are required. Missing values remain unknown. Bars compare magnitudes on this page; they do not indicate severity."/></th><th>Before → after</th><th>Observed-copy change</th><th>Evidence</th></tr></thead><tbody>{d.rows.map(r=><tr key={r.id}>
     <th scope="row">{r.bucket||r.stream}<small>{r.bucket?r.stream.startsWith("KV_")?"KV bucket":"Object store":"Stream"} · {r.account} · {r.storage}</small><small>{r.created?`Created ${stamp(r.created)}`:"Incarnation unknown"}</small></th>
     <td className="contributor-change"><span>{bytes(r.delta?.logical,true)}</span><ChangeBar value={r.delta?.logical} max={max}/></td><td>{bytes(r.before?.logical)} → {bytes(r.after?.logical)}</td><td>{bytes(r.delta?.observedReplicas,true)}</td><td><button className="btn" type="button" aria-expanded={selected?.id===r.id} aria-label={`Explore ${r.bucket||r.stream}, ${r.storage}${r.created?`, created ${stamp(r.created)}`:""}`} onClick={()=>setExpanded({selection:c.selection,id:selected?.id===r.id?"":r.id})}>Explore</button></td>
    </tr>)}</tbody></table></div>
    {!d.rows.length&&<p>No matching resource observations at either endpoint.</p>}
    <div className="page-actions"><button type="button" disabled={!c.page.previous.length||c.q.isFetching||c.normalizing} onClick={c.previous}>Previous contributors</button><button type="button" disabled={!d.nextPageToken||c.q.isFetching||c.normalizing} onClick={c.next}>Next contributors</button></div>
    {selected&&<ContributorEvidence key={`${c.selection}|${selected.id}`} row={selected} data={d}/>}
   </>;
  }}</QueryView>}
 </section>;
}
function ContributorEvidence({row:r,data:d}:{row:StorageChangeRow;data:GetStorageChangeResponse}){
 const from=stamp(d.a?.requested),to=stamp(d.b?.requested);const sys=d.system,account=r.account;
 const query=new URLSearchParams({system:sys,account,from,to,stream:`${account}/${r.stream}`});
 return <section className="contributor-evidence" aria-label={`${r.bucket||r.stream} evidence`}><h3>{r.bucket||r.stream} evidence</h3>
  <p>Before: {bytes(r.before?.logical)}. After: {bytes(r.after?.logical)}. Logical change: {bytes(r.delta?.logical,true)}.</p>
  <p>Configured replicas: {r.configuredReplicasA??"Unknown"} → {r.configuredReplicasB??"Unknown"}. Configured-copy change is an estimate: {bytes(r.delta?.estimatedReplicas,true)}.</p>
  <p>Reported processes: {r.processesA.join(", ")||"Unknown"} → {r.processesB.join(", ")||"Unknown"}. Creation: {stamp(r.created)}.</p>
  {r.reasons.map(x=><p key={x}>{reason(x)}</p>)}
  <div className="page-actions">{r.processesA.length?<Link className="x" to={resourceEndpointPath(r,d.a,sys,account,from,to)}>Configuration at A</Link>:<span>No configuration observed at A</span>}{r.processesB.length?<Link className="x" to={resourceEndpointPath(r,d.b,sys,account,from,to)}>Configuration at B</Link>:<span>No configuration observed at B</span>}<Link className="x" to={`/events?${query}`}>Events in interval</Link><Link className="x" to={`/consumers?${query}`}>Consumers in interval</Link></div>
  {r.eventIds.map(id=><p key={String(id)}><Link className="x" to={`/events?${query}&id=${id}`}>Recorded event {String(id)}</Link></p>)}
  <p>Configuration links open historical endpoint windows; consumers show the latest available collection in the interval. Charts read retained history, not a universally pinned snapshot. These observations do not establish causation.</p>
  <ContributorCharts row={r} from={from} to={to}/>
 </section>;
}
function ContributorCharts({row,from,to}:{row:StorageChangeRow;from:string;to:string}){
 const spec={kind:"fixed" as const,from:Date.parse(from),to:Date.parse(to)};
 const entity:Entity={kind:"stream",key:`${row.account}/${row.stream}`,name:row.stream,account:row.account,stream:row.stream,consumer:"",server:""};
 const queries=[{metric:"stream.bytes",entity},{metric:"stream.observed_replica_bytes",entity}];const q=useTrends(spec,queries);const f={spec,start:spec.from,end:spec.to};
 return <div className="cap-history-expanded"><OneLine f={f} q={pickTrend(q,queries,queries[0])} title="Logical bytes in interval" label="One logical copy" contiguousBuckets bucketOverlap/><OneLine f={f} q={pickTrend(q,queries,queries[1])} title="Observed copies in interval" label="Reported logical copies" contiguousBuckets bucketOverlap/></div>;
}

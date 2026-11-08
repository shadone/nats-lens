import type {ReactNode} from "react";
import {Link} from "react-router";
import {QueryView,Empty} from "./common";
import {Pager,usePager} from "./Pager";
import {useFindings} from "../api/queries";
import {useGlobalQuery} from "../lib/useGlobalQuery";
import {withGlobal} from "../lib/globalQuery";
import {writeRange} from "../lib/timeRange";
import {useSystem} from "../lib/useSystem";
import {entityName,entityPath,findingPath,findingTag} from "../lib/view";
import {Sev,StateTag} from "./Severity";
import type {Entity,FindingState} from "../api/model";

function RangeLink({to,from,end,children,account}:{to:string;from:number;end:number;children:ReactNode;account?:string}) {
 const global=useGlobalQuery();
 const [path,query]=to.split("?");
 const fixed=writeRange(new URLSearchParams(query),{kind:"fixed",from,to:end});
 const merged=new URLSearchParams(withGlobal(`${path}?${fixed}`,global).split("?")[1]);
 merged.delete("range");if(account)merged.set("account",account);
 return <Link className="x" to={`${path}?${merged}`}>{children}</Link>;
}

/** Reads the existing historical findings API, one explicit page at a time. */
export function FindingIntervalEvidence({from,end,checkCode,entity,states=["firing","changed","flapping","resolved"]}:{from:number;end:number;checkCode?:string;entity?:Entity;states?:FindingState[]}) {
 const system=useSystem();
 const pager=usePager(JSON.stringify([system,from,end,checkCode,entity?.kind,entity?.key,states]));
 const q=useFindings({kind:"fixed",from,to:end},{checkCode,entity,states,includeMuted:true,pageSize:50,pageToken:pager.token});
 const params=new URLSearchParams({state:"all"});if(checkCode)params.set("check",checkCode);
 return <section aria-label="Findings in selected interval">
  <div className="sec-h"><h3>Findings in this interval</h3><RangeLink to={`/findings?${params}`} account={entity?.account} from={from} end={end}>{entity?(checkCode?"Open check-wide findings page":"Open system-wide findings page"):"Open findings page"}</RangeLink></div>
  {entity&&<p>Selected resource: {entityName(entity)}</p>}
  <p>Includes resolved and muted findings. These are retained findings that overlapped the selected interval, not proof of an exact incident onset. Historical details reflect the available API history; a finding’s detail page may also show later observations.</p>
  <QueryView q={q} what="findings in the selected interval">{page=><>
   {page.items.length===0?<Empty>No retained findings match this interval. Evidence may have expired or not been captured.</Empty>:<div className="timeline-finding-list">{page.items.map(f=><article className="card" key={f.id}>
    <div className="sec-h"><Sev level={f.mute?"muted":f.severity}/><span className="mono">{f.checkCode}</span><strong>{f.checkName}</strong><StateTag tag={findingTag(f,from)}/></div>
    <p>{f.detail}</p>{f.value&&<p>{f.value.name}: {f.value.display}</p>}
    <p>{entityName(f.entity)||"System"}{f.mute&&" · Muted"}</p>
    <div className="btns"><RangeLink to={findingPath(f.id)} account={f.entity?.account??entity?.account} from={from} end={end}>Open finding details</RangeLink>{f.entity&&entityPath(f.entity)&&<RangeLink to={entityPath(f.entity)!} account={f.entity.account} from={from} end={end}>Open affected resource</RangeLink>}<RangeLink to={`/checks/${encodeURIComponent(f.checkCode)}`} from={from} end={end}>Read check reference</RangeLink></div>
    {f.remediation&&<details><summary>Remediation</summary><p>{f.remediation}</p></details>}
   </article>)}</div>}
   <Pager page={pager.page} next={page.nextPageToken} total={page.totalCount} shown={page.items.length} onNext={()=>pager.next(page.nextPageToken)} onPrev={pager.prev}/>
  </>}</QueryView>
 </section>;
}

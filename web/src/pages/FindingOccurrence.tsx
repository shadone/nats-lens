import {InvestigationButton} from '../components/InvestigationContext';
import {useState} from 'react';
import {Link,useParams,useSearchParams} from 'react-router';
import {epoch,useFindingEpisodes,type FindingEpisode} from '../api/findingEpisodes';
import {OccurrenceSummary,observed,stateLabel} from '../components/TimelineFindingEpisodes';
import {QueryView,Empty} from '../components/common';
import {findingPath,entityPath} from '../lib/view';
import {findingHistoryRange} from '../lib/findingHistory';
import type {FrozenEvidenceRange} from '../api/model';
import {useSystem} from '../lib/useSystem';
import {usePageTitle} from '../lib/usePageTitle';

/** A shared link names the occurrence and fixed interval; current finding state
 * is an explicitly separate destination, never a replacement for expired history. */
export function FindingOccurrence(){
 usePageTitle('Finding occurrence');const {id='',firstEpoch=''}=useParams();const [p]=useSearchParams();
 let selected:ReturnType<typeof findingHistoryRange>|undefined;
 try{if(!id||epoch(firstEpoch)===0n)throw new Error();selected=findingHistoryRange(p)}catch{/* Invalid evidence bounds never broaden the read. */}
 return <><h1>Finding occurrence</h1>{!selected?<p role="alert">This occurrence link requires a valid retained identity and fixed from/to interval.</p>:<OccurrenceEvidence key={JSON.stringify([id,firstEpoch,selected])} id={id} firstEpoch={firstEpoch} {...selected}/>}</>;
}
function OccurrenceEvidence({id,firstEpoch,from,end,readEpoch,range}:{id:string;firstEpoch:string;from:number;end:number;readEpoch?:string;range:FrozenEvidenceRange}){
 const system=useSystem();const [pages,setPages]=useState([{offset:0,token:''}]);const current=pages[pages.length-1];
 const q=useFindingEpisodes(from,end,{range,findingId:id,firstEpoch,readEpoch,includeTransitions:true,pageSize:1,pageToken:current.token,transitionOffset:current.offset});
 const fixed=(path:string)=>`${path}?${new URLSearchParams({system,from:range.start,to:range.end})}`;
 return <><p>{observed(from)} – {observed(end)}. Historical observations and the latest retained state are shown separately. This link does not pin every database fact; expired history is never replaced by another recurrence.</p><QueryView q={q} what="selected finding occurrence">{page=>{const e=page.items[0];return e?<article className="card finding-occurrence"><div className="btns"><InvestigationButton label selection={{range,check:e.finding.checkCode,entity:e.finding.entity,start:from,end,query:{method:'ListFindingEpisodes',request:{range,findingId:id,firstRetainedEpoch:firstEpoch,readEpoch:page.readEpoch,includeTransitions:true,pageSize:1,pageToken:current.token,transitionOffset:current.offset}},evidence:[{kind:'finding',id}],annotation:'Selected retained occurrence; historical state differs from latest retained state. No exact condition onset or complete capture is implied.'}}/></div><OccurrenceSummary e={e} end={end}/><p className="mono">First retained transition identity: {e.firstEpoch} · Transition boundary: {page.readEpoch}</p><div className="btns"><Link className="x" to={fixed(findingPath(id))}>Open latest finding details</Link>{e.finding.entity&&entityPath(e.finding.entity)&&<Link className="x" to={fixed(entityPath(e.finding.entity)!)}>Open affected resource</Link>}<Link className="x" to={fixed(`/checks/${encodeURIComponent(e.finding.checkCode)}`)}>Read check reference</Link>{e.causeEventId!=='0'&&<Link className="x" to={`${fixed('/events')}&id=${encodeURIComponent(e.causeEventId)}`}>Open known cause event</Link>}</div><h2>Recorded transitions</h2><p>{e.transitions.length} shown of {e.transitionCount} retained transitions, newest collection first. Times may be non-monotonic; collection order is preserved.</p><div className="table-wrap"><table className="tbl"><thead><tr><th scope="col">Transition identity</th><th scope="col">Time, UTC</th><th scope="col">State</th><th scope="col">Severity</th><th scope="col">Recorded detail</th></tr></thead><tbody>{e.transitions.map(t=><tr key={t.epoch}><td className="mono">{t.epoch}</td><td>{observed(t.time)}</td><td>{stateLabel(t.state)}</td><td>{t.severity}</td><td>{t.detail}</td></tr>)}</tbody></table></div><nav className="btns" aria-label="Transition history pages"><button className="btn" disabled={pages.length===1} onClick={()=>setPages(p=>p.slice(0,-1))}>Newer transitions</button><button className="btn" disabled={!e.nextTransitionOffset} onClick={()=>setPages(p=>[...p,{offset:e.nextTransitionOffset,token:e.transitionToken}])}>Older transitions</button></nav><Unavailable e={e}/>{e.finding.remediation&&<details><summary>Current check guidance</summary><p>{e.finding.remediation}</p></details>}</article>:<Empty>This exact occurrence has no retained evidence in the selected interval. It may have expired or not been captured. No later occurrence has been substituted.</Empty>}}</QueryView></>;
}
function Unavailable({e}:{e:FindingEpisode}){return e.unavailable.length>0?<details><summary>Unavailable evidence</summary><ul>{e.unavailable.map(x=><li key={x}>{x.replaceAll('_',' ')}</li>)}</ul><p>Missing historical configuration, operator state or onset is not reconstructed from current values.</p></details>:null}

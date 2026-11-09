import {Link} from 'react-router';
import {useFindingEpisodes,type FindingEpisode} from '../api/findingEpisodes';
import type {FrozenEvidenceRange,Entity} from '../api/model';
import {usePager,Pager} from './Pager';
import {QueryView,Empty} from './common';
import {Sev} from './Severity';
import {useSystem} from '../lib/useSystem';
import {entityName,entityPath} from '../lib/view';
import {timelineTime} from './TimelineMark';
import {findingHistoryPath} from '../lib/findingHistory';
import {writeRange} from '../lib/timeRange';
export function occurrencePath(id:string,firstEpoch:string,from:number,end:number,readEpoch:string,system:string,range?:FrozenEvidenceRange){
 const p=writeRange(new URLSearchParams({system,readEpoch}),{kind:'fixed',from,to:end});
 if(range){p.set('from',range.start);p.set('to',range.end)}
 return `/findings/${encodeURIComponent(id)}/occurrences/${firstEpoch}?${p}`;
}
export const stateLabel=(state:string)=>state==='unknown'?'Unavailable':state.charAt(0).toUpperCase()+state.slice(1);
export const observed=(t:number|undefined)=>t===undefined?'Unavailable':timelineTime(t);
export function OccurrenceSummary({e,end}:{e:FindingEpisode;end:number}){
 return <><div className="sec-h"><Sev level={e.finding.severity}/><span className="mono">{e.finding.checkCode}</span><strong>{e.finding.checkName}</strong></div><p>{e.finding.detail}</p><p>{entityName(e.finding.entity)||'System'}</p><dl className="kv occurrence-facts"><dt>Recorded state before {timelineTime(end)}</dt><dd>{stateLabel(e.finding.state)}</dd><dt>First retained observation</dt><dd>{observed(e.firstAt)}{!e.startEpoch&&' · Original firing transition unavailable'}</dd><dt>Observed resolution</dt><dd>{e.resolutionAt===undefined?'No retained resolution':observed(e.resolutionAt)}</dd></dl><p>Latest retained state: {stateLabel(e.latestState)} · {observed(e.latestAt)}. This is separate from the selected historical occurrence.</p></>;
}
export function TimelineFindingEpisodes({from,end,checkCode,entity,readEpoch,range,showAllResultsLink=true}:{from:number;end:number;checkCode?:string;entity?:Entity;readEpoch?:string;range?:FrozenEvidenceRange;showAllResultsLink?:boolean}){
 const system=useSystem();const pager=usePager(JSON.stringify([system,from,end,checkCode,entity?.kind,entity?.key,readEpoch,range]));
 const q=useFindingEpisodes(from,end,{checkCode,entity,readEpoch,range,pageSize:50,pageToken:pager.token});
 return <section aria-label="Finding occurrences in selected interval"><h3>Finding occurrences in this interval</h3>{showAllResultsLink&&(readEpoch!==undefined||q.data)?<p><Link className="x" to={findingHistoryPath(from,end,readEpoch??q.data?.readEpoch,system,checkCode,entity,range)}>Open all retained occurrences</Link></p>:null}<p>Retained occurrences overlapping the fixed interval, including recorded resolutions. Repeated occurrences remain separate. Recorded observations do not prove an uninterrupted condition or exact onset.</p>{entity&&<p>Selected resource: {entityName(entity)}</p>}<QueryView q={q} what="finding occurrence history">{page=><>{page.items.length===0?<Empty>No retained occurrences match this interval. Evidence may have expired or not been captured.</Empty>:<div className="timeline-finding-list">{page.items.map(e=><article className="card" key={`${e.finding.id}:${e.firstEpoch}`}><OccurrenceSummary e={e} end={end}/><div className="btns"><Link className="x" to={occurrencePath(e.finding.id,e.firstEpoch,from,end,page.readEpoch,system,range)}>Open this occurrence</Link>{e.finding.entity&&entityPath(e.finding.entity)&&<Link className="x" to={`${entityPath(e.finding.entity)}?${writeRange(new URLSearchParams({system,account:e.finding.entity.account}),{kind:'fixed',from,to:end})}`}>Open affected resource</Link>}</div></article>)}</div>}<Pager page={pager.page} next={page.nextPageToken} total={page.totalCount} shown={page.items.length} onNext={()=>pager.next(page.nextPageToken)} onPrev={pager.prev}/></>}</QueryView></section>;
}

import {useParams,useSearchParams} from 'react-router';
import {useTimelineWindow} from '../api/timelineEvidence';
import {eventReadId,frozenEventRange} from '../api/eventIdentity';
import {TimelineWindowEvidence} from '../components/TimelineEventEvidence';
import {QueryView} from '../components/common';
import {InvestigationButton} from '../components/InvestigationContext';
import {usePageTitle} from '../lib/usePageTitle';
export function TimelineWindow(){
 usePageTitle('Timeline window evidence');const {id=''}=useParams(),[p]=useSearchParams();const range={start:p.get('from')??'',end:p.get('to')??''},readId=p.get('eventReadId')??'';
 let valid=true;try{if(!/^[a-f0-9]{64}$/.test(id)||!readId||Date.parse(range.end)<=Date.parse(range.start))throw new Error();frozenEventRange(range);eventReadId(readId)}catch{valid=false}
 return <><h1>Timeline window evidence</h1>{valid?<WindowEvidence key={JSON.stringify([id,range,readId])} id={id} range={range} readId={readId}/>:<p role="alert">This window requires its captured identity, event boundary and fixed from/to interval.</p>}</>;
}
function WindowEvidence({id,range,readId}:{id:string;range:{start:string;end:string};readId:string}){
 const q=useTimelineWindow(id,range,readId);
 return <><p>A detected window groups recorded observations. Its bounds do not establish a maintenance duration, complete capture, or causation. Retention can remove the original support; another window is never substituted.</p><QueryView q={q} what="captured Timeline window">{r=>r.window?<article className="card finding-occurrence"><h2>{r.window.label}</h2><p>{r.window.detail}</p><InvestigationButton label selection={{range,start:Date.parse(range.start),end:Date.parse(range.end),query:{method:'GetStatusTimeline',request:{eventReadId:readId}},annotation:`Captured inferred window ${id}; exact support is in supportingEventIds. No causal claim.`}}/><TimelineWindowEvidence window={r.window} range={range} readId={readId}/></article>:<p role="status">This exact inferred window cannot be reconstructed from the retained captured inputs. Its support may have expired, changed, or exceeded an input limit. No current window replaces it.</p>}</QueryView></>;
}

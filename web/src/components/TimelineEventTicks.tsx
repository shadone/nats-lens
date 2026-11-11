import type {FrozenEvidenceRange,LaneEvent} from '../api/model';
import {groupTimelineEvents,eventSourceLabel} from '../lib/timelineEvidence';
import {TimelineMark,EventPreview,EventDetails} from './TimelineMark';
import type {TimelineCaptureLimit} from './TimelineCaptureNotice';
import {CapturedEventList} from './TimelineEventEvidence';
/** Collision groups describe visual overlap only; every captured member is retained. */
/** name names a group's lane and markLabel a single mark, for lanes other
 * than Kubernetes and Events. */
export function TimelineEventTicks({events,from,end,width,range,readId,className,captureLimits,name,markLabel}:{events:LaneEvent[];from:number;end:number;width:number;range:FrozenEvidenceRange;readId?:string;className:(e:LaneEvent)=>string;captureLimits?:TimelineCaptureLimit[];name?:string;markLabel?:(e:LaneEvent)=>string}){
 return <>{groupTimelineEvents(events,from,end,width).map(g=>{
 const e=g.events[0],many=g.events.length>1;
 return <TimelineMark captureLimits={captureLimits} key={g.key} className={`${className(e)}${many?' timeline-event-group':''}`} style={{left:`${100*(g.center-from)/(end-from)}%`}} label={many?`${g.events.length} overlapping ${name??(e.source==='kubernetes'?'Kubernetes':'Events')} marks`:markLabel?markLabel(e):`${eventSourceLabel(e.source)} event: ${e.label}`} preview={many?<><strong>{g.events.length} overlapping observations</strong><p>Visual overlap, not one incident.</p>{g.events.slice(0,5).map(x=><div key={x.id}>{x.label}</div>)}{g.events.length>5&&<div>Activate to read all captured members.</div>}</>:<EventPreview event={e}/>} details={many?<CapturedEventList ids={[...new Set(g.events.flatMap(x=>x.eventId?[x.eventId]:[]))]} captured={g.events} range={range} readId={readId} captureLimits={captureLimits}/>:<EventDetails event={e} range={range} readId={readId} captureLimits={captureLimits}/>}>{many&&<span>{g.events.length}</span>}</TimelineMark>;
 })}</>;
}

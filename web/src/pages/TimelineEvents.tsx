import {Link,useLocation} from 'react-router';
import {selectionFromURL,selectionPath,type CapturedSelection} from '../lib/capturedEventSelection';
import {useCapturedSelectionPage} from '../api/timelineEvidence';
import {CapturedEventRows} from '../components/TimelineEventEvidence';
import {TimelineCaptureNotice,captureLimitText} from '../components/TimelineCaptureNotice';
import {InvestigationButton} from '../components/InvestigationContext';
import {QueryView} from '../components/common';
import {usePageTitle} from '../lib/usePageTitle';
export function TimelineEvents(){
 usePageTitle('Captured Timeline events');const loc=useLocation();let selected:ReturnType<typeof selectionFromURL>|undefined;
 try{selected=selectionFromURL(new URL(loc.pathname+loc.search+loc.hash,'https://example.com'))}catch{/* Invalid links never become a current event-feed read. */}
 return <><h1>Captured Timeline events</h1>{selected?<Selection key={JSON.stringify(selected)} {...selected}/>:<p role="alert">This captured selection link is invalid or its system/range has changed. Reopen the original captured selection.</p>}</>;
}
function Selection({selection:s,offset,pageSize}:{selection:CapturedSelection;offset:number;pageSize:number}){
 const q=useCapturedSelectionPage(s,offset,pageSize),range={start:s.from,end:s.to},shown=s.ids.slice(offset,offset+pageSize),next=offset+shown.length;
 return <section className="card"><h2>Captured membership</h2><p>{s.ids.length} captured event identities. These are the observations supplied by the original visual group, not all events in the interval or one proven incident.</p><p>Context interval: {s.from} → {s.to}. Supporting identities are read directly, without an additional time filter.</p><p>{s.eventReadId!==undefined?`Event membership boundary: ${s.eventReadId}. Later IDs are excluded; changes or deletion of existing rows are not pinned.`:'No event membership boundary was supplied; recorded identities are re-read without a captured source boundary.'}</p><TimelineCaptureNotice limits={s.limits}/><p>Share this browser URL to reopen the captured membership and current page. Its fragment contains event IDs and context; share only with intended recipients. Missing records remain unavailable; a current feed never replaces them.</p><InvestigationButton label selection={{range,start:Date.parse(s.from),end:Date.parse(s.to),query:{method:'ListEventSelection',request:{ids:s.ids,offset,pageSize,...(s.eventReadId!==undefined?{eventReadId:s.eventReadId}:{})}},annotation:['Explicit captured Timeline membership; one summary page, payloads only on demand. No complete-capture or causal claim.',captureLimitText(s.limits)].filter(Boolean).join(' ')}}/><QueryView q={q} what="captured event selection">{data=><><p>{data.totalCount} retained records available on this page of {shown.length} selected identities.</p><CapturedEventRows ids={shown} items={data.items} range={range} readId={s.eventReadId} captureLimits={s.limits} offset={offset}/></>}</QueryView><nav className="btns" aria-label="Captured selection pages">{offset>0&&<Link className="btn" to={selectionPath(s,Math.max(0,offset-pageSize),pageSize)}>Previous captured events</Link>}<span>{offset+1}–{next} of {s.ids.length}</span>{next<s.ids.length&&<Link className="btn" to={selectionPath(s,next,pageSize)}>Next captured events</Link>}</nav></section>;
}

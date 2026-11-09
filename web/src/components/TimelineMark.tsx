import {useEffect, useId, useRef, useState, type CSSProperties, type ReactNode} from "react";
import {createPortal} from "react-dom";
import {TimelineCaptureNotice,type TimelineCaptureLimit} from "./TimelineCaptureNotice";
import {useTimelineInvestigation} from "./TimelineInvestigation";
import {Dialog} from "./Dialog";
import {RLink} from "./common";
import {useReadoutHover} from "../charts/useReadoutHover";
import {ChartReadout} from "../charts/ChartReadout";
import {isoSecond} from "../lib/format";
import {RecordedTimelineEvent} from "./TimelineEventEvidence";
import type {FrozenEvidenceRange,LaneEvent} from "../api/model";

export const timelineTime=(time:number)=>`${isoSecond(time)} UTC`;

/** Shared viewport preview for HTML lane marks and SVG cells/zoom marks. */
export function TimelineMark({label,preview,children,className,style,svg=false,details,captureLimits}:{label:string;preview:ReactNode;children?:ReactNode;className?:string;style?:CSSProperties;svg?:boolean;details?:ReactNode;captureLimits?:TimelineCaptureLimit[]}) {
  const select=useTimelineInvestigation();
  const anchor=useRef<Element|null>(null);
  const id=useId();
  const [open,setOpen]=useState(false);
  const [selected,setSelected]=useState(false);
  useEffect(()=>{
    if(!open)return;
    const escape=(e:KeyboardEvent)=>{if(e.key==="Escape"){setOpen(false);setSelected(false)}};
    const outside=(e:PointerEvent)=>{if(!anchor.current?.contains(e.target as Node)&&!document.getElementById(id)?.contains(e.target as Node)){setOpen(false);setSelected(false)}};
    document.addEventListener("keydown",escape);document.addEventListener("pointerdown",outside);
    return ()=>{document.removeEventListener("keydown",escape);document.removeEventListener("pointerdown",outside)};
  },[open]);
  const presence=useReadoutHover(anchor,id,()=>setOpen(false));
  const visiblePreview=<>{preview}<TimelineCaptureNotice limits={captureLimits}/></>;
  const activate=()=>{setOpen(false);if(select)select({label,preview:visiblePreview,details});else setSelected(true)};
  const close=()=>{setSelected(false);setOpen(false)};
  const props={className:`timeline-mark ${className??""}`,style,"aria-label":label,"aria-describedby":open?id:undefined,"aria-haspopup":"dialog" as const,
    onPointerEnter:()=>{presence.keep();if(!selected)setOpen(true)},onPointerLeave:presence.leave,onFocus:()=>{if(!selected)setOpen(true)},onBlur:(e:{relatedTarget:EventTarget|null})=>presence.blur(e.relatedTarget),onClick:activate};
  const modal=selected?createPortal(<Dialog open title={label} onClose={close}><div className="timeline-details">{visiblePreview}{details}</div></Dialog>,document.body):null;
  const tip=open&&!selected?<ChartReadout anchor={anchor} fraction={.5} id={id} contentKey={label} hover={presence}>{visiblePreview}</ChartReadout>:null;
  return svg?<><g {...props} ref={el=>{anchor.current=el}} role="button" tabIndex={0} onKeyDown={e=>{if(e.key==="Enter"||e.key===" "){e.preventDefault();activate()}}}>{children}</g>{tip}{modal}</>:<><button {...props} ref={el=>{anchor.current=el}} type="button">{children}</button>{tip}{modal}</>;
}

export function EventDetails(props:{event:LaneEvent;range?:FrozenEvidenceRange;readId?:string;captureLimits?:TimelineCaptureLimit[]}) {
 return <RecordedTimelineEvent {...props}/>;
}
export function CheckDetails({code}:{code:string}) {
  return <p><RLink to={`/checks/${encodeURIComponent(code)}`}>Read check reference</RLink></p>;
}

export function EventPreview({event:e}:{event:LaneEvent}) {
  return <><strong>{e.label}</strong><div>{timelineTime(e.time)}</div><div>{e.source}: {e.kind}</div>{e.severity&&<div>Severity: {e.severity}</div>}{e.eventId&&<div>Event ID: {e.eventId}</div>}</>;
}
export function WindowPreview({label,start,end,detail}:{label:string;start:number;end:number;detail?:string}) {
  return <><strong>{label}</strong><div>{timelineTime(start)} – {timelineTime(end)}</div>{detail&&<p>{detail}</p>}<div>Inferred window; these bounds do not establish a cause.</div></>;
}

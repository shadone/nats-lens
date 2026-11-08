import {useEffect, useId, useRef, useState, type CSSProperties, type ReactNode} from "react";
import {createPortal} from "react-dom";
import {useTimelineInvestigation} from "./TimelineInvestigation";
import {Dialog} from "./Dialog";
import {RLink} from "./common";
import {ChartReadout} from "../charts/ChartReadout";
import {isoSecond} from "../lib/format";
import type {LaneEvent} from "../api/model";

export const timelineTime=(time:number)=>`${isoSecond(time)} UTC`;

/** Shared viewport preview for HTML lane marks and SVG cells/zoom marks. */
export function TimelineMark({label,preview,children,className,style,svg=false,details}:{label:string;preview:ReactNode;children?:ReactNode;className?:string;style?:CSSProperties;svg?:boolean;details?:ReactNode}) {
  const select=useTimelineInvestigation();
  const anchor=useRef<Element|null>(null);
  const id=useId();
  const [open,setOpen]=useState(false);
  const [selected,setSelected]=useState(false);
  useEffect(()=>{
    if(!open)return;
    const escape=(e:KeyboardEvent)=>{if(e.key==="Escape"){setOpen(false);setSelected(false)}};
    const outside=(e:PointerEvent)=>{if(!anchor.current?.contains(e.target as Node)){setOpen(false);setSelected(false)}};
    document.addEventListener("keydown",escape);document.addEventListener("pointerdown",outside);
    return ()=>{document.removeEventListener("keydown",escape);document.removeEventListener("pointerdown",outside)};
  },[open]);
  const activate=()=>{setOpen(false);if(select)select({label,preview,details});else setSelected(true)};
  const close=()=>{setSelected(false);setOpen(false)};
  const props={className:`timeline-mark ${className??""}`,style,"aria-label":label,"aria-describedby":open?id:undefined,"aria-haspopup":"dialog" as const,
    onPointerEnter:()=>{if(!selected)setOpen(true)},onPointerLeave:()=>setOpen(false),onFocus:()=>{if(!selected)setOpen(true)},onBlur:()=>setOpen(false),onClick:activate};
  const modal=selected?createPortal(<Dialog open title={label} onClose={close}><div className="timeline-details">{preview}{details}</div></Dialog>,document.body):null;
  const tip=open&&!selected?<ChartReadout anchor={anchor} fraction={.5} id={id} contentKey={label}>{preview}</ChartReadout>:null;
  return svg?<><g {...props} ref={el=>{anchor.current=el}} role="button" tabIndex={0} onKeyDown={e=>{if(e.key==="Enter"||e.key===" "){e.preventDefault();activate()}}}>{children}</g>{tip}{modal}</>:<><button {...props} ref={el=>{anchor.current=el}} type="button">{children}</button>{tip}{modal}</>;
}

export function EventDetails({event:e}:{event:LaneEvent}) {
  return e.eventId?<p><RLink to={`/events?id=${encodeURIComponent(e.eventId)}`}>Open recorded event</RLink></p>:<p>Only the Timeline summary is available here; this mark has no recorded-event ID.</p>;
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

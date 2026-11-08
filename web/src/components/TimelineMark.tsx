import {useEffect, useId, useRef, useState, type CSSProperties, type ReactNode} from "react";
import {ChartReadout} from "../charts/ChartReadout";
import {isoSecond} from "../lib/format";
import type {LaneEvent} from "../api/model";

export const timelineTime=(time:number)=>`${isoSecond(time)} UTC`;

/** Shared viewport preview for HTML lane marks and SVG cells/zoom marks. */
export function TimelineMark({label,preview,children,className,style,svg=false}:{label:string;preview:ReactNode;children?:ReactNode;className?:string;style?:CSSProperties;svg?:boolean}) {
  const anchor=useRef<Element|null>(null);
  const id=useId();
  const [open,setOpen]=useState(false);
  const [pinned,setPinned]=useState(false);
  useEffect(()=>{
    if(!open)return;
    const escape=(e:KeyboardEvent)=>{if(e.key==="Escape"){setOpen(false);setPinned(false)}};
    const outside=(e:PointerEvent)=>{if(!anchor.current?.contains(e.target as Node)){setOpen(false);setPinned(false)}};
    document.addEventListener("keydown",escape);document.addEventListener("pointerdown",outside);
    return ()=>{document.removeEventListener("keydown",escape);document.removeEventListener("pointerdown",outside)};
  },[open]);
  const activate=()=>{setPinned(!pinned);setOpen(!pinned)};
  const props={className:`timeline-mark ${className??""}`,style,"aria-label":label,"aria-describedby":open?id:undefined,
    onPointerEnter:()=>setOpen(true),onPointerLeave:()=>{if(!pinned)setOpen(false)},onFocus:()=>setOpen(true),onBlur:()=>{setOpen(false);setPinned(false)},onClick:activate};
  const tip=open?<ChartReadout anchor={anchor} fraction={.5} id={id} contentKey={label}>{preview}</ChartReadout>:null;
  return svg?<><g {...props} ref={el=>{anchor.current=el}} role="button" tabIndex={0} onKeyDown={e=>{if(e.key==="Enter"||e.key===" "){e.preventDefault();activate()}}}>{children}</g>{tip}</>:<><button {...props} ref={el=>{anchor.current=el}} type="button">{children}</button>{tip}</>;
}

export function EventPreview({event:e}:{event:LaneEvent}) {
  return <><strong>{e.label}</strong><div>{timelineTime(e.time)}</div><div>{e.source}: {e.kind}</div>{e.severity&&<div>Severity: {e.severity}</div>}{e.eventId&&<div>Event ID: {e.eventId}</div>}</>;
}
export function WindowPreview({label,start,end}:{label:string;start:number;end:number}) {
  return <><strong>{label}</strong><div>{timelineTime(start)} – {timelineTime(end)}</div><div>Inferred window; these bounds do not establish a cause.</div></>;
}

import {createContext,useCallback,useContext,useId,useLayoutEffect,useMemo,useRef,useState,type ReactNode,type PointerEvent,type KeyboardEvent,type MouseEvent} from "react";
import {useLocation,useNavigate,useSearchParams} from "react-router";
import {useCurrentSystem} from "../api/systems";
import {Dialog} from "../components/Dialog";
import {InvestigationButton} from "../components/InvestigationContext";
import type {InvestigationSelection} from "../lib/investigationContext";
import {chartInterval,intervalProblem,parseUTCInput,supportsStorageChange,utcInput,validChartFrame,type ChartInterval} from "../lib/chartInterval";
import {metricDestination} from "../lib/metricNavigation";
import {useTimeRange} from "../lib/useTimeRange";

interface SelectionInput {scope:string;chartId:string;label:string;source?:InvestigationSelection;frame:ChartInterval;range:ChartInterval}
interface Choice extends SelectionInput {id:number;system:string;account:string;highlight?:ChartInterval}
interface Host {scope:string;selection?:Choice;open:(input:SelectionInput)=>void}
const IntervalHost=createContext<Host|undefined>(undefined);

/** One explicit action dialog survives data-refresh removal of its plot. */
export function ChartIntervalProvider({children}:{children:ReactNode}){
 const location=useLocation(),system=useCurrentSystem();const [params]=useSearchParams();const {setSpec}=useTimeRange();const navigate=useNavigate();
 const ordered=new URLSearchParams(params);ordered.sort();const scope=JSON.stringify([location.key,location.pathname,ordered.toString(),system.id]);
 const [choice,setChoice]=useState<Choice>();const sequence=useRef(0);
 const open=useCallback((input:SelectionInput)=>{
  if(input.scope!==scope||!validChartFrame(input.frame))return;
  const candidates=[input.source?.entity,...(input.source?.series?.map(x=>x.entity)??[])].map(x=>x?.account).filter((x):x is string=>!!x);
  const unique=[...new Set(candidates)];const account=unique.length===1?unique[0]:params.get("account")??"";
  setChoice({...input,id:++sequence.current,system:system.id,account,highlight:input.range});
 },[scope,system.id,params]);
 useLayoutEffect(()=>{setChoice(undefined)},[scope]);
 const visible=choice?.scope===scope?choice:undefined;
 const host=useMemo(()=>({scope,selection:visible,open}),[scope,visible,open]);
 const close=()=>setChoice(undefined);
 return <IntervalHost.Provider value={host}>{children}{visible&&<IntervalDialog key={visible.id} choice={visible} onClose={close} onEdit={range=>setChoice(c=>c?.id===visible.id?{...c,highlight:range}:c)} onSet={range=>{close();setSpec({kind:"fixed",from:range.start,to:range.end},{push:true})}} onInvestigate={(range,metric)=>{
   // ASVS 1.2.2: only a fixed local route and encoded scope/times are navigated.
   const p=new URLSearchParams({system:visible.system,account:visible.account,from:new Date(range.start).toISOString(),to:new Date(range.end).toISOString(),contributors:"1"});
   const destination=metricDestination(visible.source);if(metric&&destination?.startsWith("/metrics/")){const [path,own]=destination.split("?");new URLSearchParams(own).forEach((v,k)=>p.set(k,v));p.delete("contributors");close();navigate(`${path}?${p}`)}else{close();navigate(`/capacity/storage-change?${p}`)}
  }}/>}</IntervalHost.Provider>;
}
function IntervalDialog({choice,onClose,onEdit,onSet,onInvestigate}:{choice:Choice;onClose:()=>void;onEdit:(range:ChartInterval|undefined)=>void;onSet:(range:ChartInterval)=>void;onInvestigate:(range:ChartInterval,metric?:boolean)=>void}){
 const [from,setFrom]=useState(utcInput(choice.range.start)),[to,setTo]=useState(utcInput(choice.range.end));
 const a=parseUTCInput(from),b=parseUTCInput(to);const range=a!==undefined&&b!==undefined?{start:a,end:b}:undefined;const problem=intervalProblem(range,choice.frame);
 const edit=(nextFrom:string,nextTo:string)=>{setFrom(nextFrom);setTo(nextTo);const start=parseUTCInput(nextFrom),end=parseUTCInput(nextTo);const next=start!==undefined&&end!==undefined?{start,end}:undefined;onEdit(intervalProblem(next,choice.frame)?undefined:next)};
 return <Dialog open title="Selected chart interval" onClose={onClose}>
  <p>{choice.label}. Times are UTC. No page range changes until you choose an action.</p>
  <form onSubmit={e=>{e.preventDefault();if(range&&!problem)onSet(range)}}>
   <label className="field">Start (UTC)<input aria-label="Start (UTC)" type="datetime-local" step="0.001" min={utcInput(choice.frame.start)} max={utcInput(choice.frame.end)} value={from} onChange={e=>edit(e.target.value,to)}/></label>
   <label className="field">End (UTC)<input aria-label="End (UTC)" type="datetime-local" step="0.001" min={utcInput(choice.frame.start)} max={utcInput(choice.frame.end)} value={to} onChange={e=>edit(from,e.target.value)}/></label>
   {problem&&<p role="alert">{problem}</p>}
   <p>{choice.system||"Resolving system"}{choice.account?` · account ${choice.account}`:" · all accounts"}. Selecting an interval does not establish a cause.</p>
   <div className="dlg-btns chart-interval-actions"><button type="button" className="btn" onClick={onClose}>Cancel</button><button className="btn" type="submit" disabled={!!problem}>Set time range</button>{metricDestination(choice.source)?.startsWith("/metrics/")&&<button className="btn" type="button" disabled={!!problem||!choice.system} onClick={()=>{if(range&&!problem)onInvestigate(range,true)}}>Open metric details</button>}{supportsStorageChange(choice.source)&&<button className="btn" type="button" disabled={!!problem||!choice.system} onClick={()=>{if(range&&!problem)onInvestigate(range)}}>Investigate change</button>}{range&&!problem&&<InvestigationButton label selection={{...choice.source,start:range.start,end:range.end,point:undefined,label:choice.label}}/>}</div>
  </form>
 </Dialog>;
}
interface Gesture {key:string;scope:string;target:HTMLDivElement;pointerId:number;x:number;y:number;left:number;width:number;frame:ChartInterval;moved:boolean}
export function useChartIntervalInteraction({label,start,end,source,enabled}:{label:string;start:number;end:number;source?:InvestigationSelection;enabled?:boolean}){
 const host=useContext(IntervalHost),chartId=useId();const frame={start,end};const eligible=!!host&&validChartFrame(frame)&&(enabled??(supportsStorageChange(source)||!!metricDestination(source)?.startsWith("/metrics/")));
 const key=JSON.stringify([host?.scope,label,source?.metric,source?.entity?.key,source?.series?.map(x=>[x.metric,x.entity?.key])]);
 const gesture=useRef<Gesture|undefined>(undefined);const keyboard=useRef<{anchor:number;cursor:number;frame:ChartInterval;step:number}|undefined>(undefined);const blockClick=useRef(0);const [draft,setDraft]=useState<{key:string;range:ChartInterval;frame:ChartInterval}>();
 const release=(g:Gesture)=>{try{if(g.target.hasPointerCapture?.(g.pointerId))g.target.releasePointerCapture(g.pointerId)}catch{/* Pointer already ended. */}};
 const cancel=()=>{const g=gesture.current;gesture.current=undefined;keyboard.current=undefined;setDraft(undefined);if(g){if(g.moved)blockClick.current=performance.now()+500;release(g)}};
 useLayoutEffect(()=>{cancel()},[key]);
 useLayoutEffect(()=>()=>{const g=gesture.current;gesture.current=undefined;if(g)release(g)},[]);
 const time=(x:number,g:Gesture)=>g.frame.start+Math.max(0,Math.min(1,(x-g.left)/g.width))*(g.frame.end-g.frame.start);
 const show=(range:ChartInterval,selectedFrame=frame)=>{if(!host||!eligible)return;host.open({scope:host.scope,chartId,label,source,frame:selectedFrame,range});setDraft(undefined);keyboard.current=undefined};
 if(!eligible)return;
 const selected=draft?.key===key?draft.range:host?.selection?.chartId===chartId?host.selection.highlight:undefined;
 return {highlight:selected,active:!!selected||!!host?.selection,
  picker:()=>{const range=chartInterval(start,end,frame);if(range)show(range)},
  down:(e:PointerEvent<HTMLDivElement>)=>{blockClick.current=0;if(e.isPrimary===false||e.button!==0||e.ctrlKey||e.metaKey)return;const box=e.currentTarget.getBoundingClientRect();if(box.width<=0)return;gesture.current={key,scope:host!.scope,target:e.currentTarget,pointerId:e.pointerId,x:e.clientX,y:e.clientY,left:box.left,width:box.width,frame,moved:false};keyboard.current=undefined;setDraft(undefined)},
  move:(e:PointerEvent<HTMLDivElement>)=>{const g=gesture.current;if(!g||g.pointerId!==e.pointerId||g.key!==key)return;const dx=e.clientX-g.x,dy=e.clientY-g.y;if(!g.moved&&Math.abs(dy)>Math.abs(dx)&&Math.abs(dy)>=4&&e.pointerType==="touch"){cancel();return}if(Math.abs(dx)<4&&!g.moved)return;if(!g.moved){g.moved=true;g.target.focus({preventScroll:true});try{g.target.setPointerCapture?.(g.pointerId)}catch{/* Inspection still works without capture. */}}e.preventDefault();const range=chartInterval(time(g.x,g),time(e.clientX,g),g.frame);setDraft(range?{key,range,frame:g.frame}:undefined)},
  up:(e:PointerEvent<HTMLDivElement>)=>{const g=gesture.current;if(!g||g.pointerId!==e.pointerId)return;gesture.current=undefined;release(g);if(g.key!==key||g.scope!==host!.scope)return;if(Math.abs(e.clientX-g.x)<4&&!g.moved)return;blockClick.current=performance.now()+500;const range=chartInterval(time(g.x,g),time(e.clientX,g),g.frame);if(range)show(range,g.frame)},
  cancel:()=>{if(gesture.current)cancel()},
  click:(e:MouseEvent<HTMLDivElement>)=>{if(blockClick.current>performance.now()){blockClick.current=0;e.preventDefault();e.stopPropagation()}},
  key:(e:KeyboardEvent<HTMLDivElement>,current:number,step:number)=>{
   if(e.target!==e.currentTarget)return false;
   if(e.key==="Escape"){cancel();return false}
   const direction=e.key==="ArrowRight"||e.key==="ArrowUp"?1:e.key==="ArrowLeft"||e.key==="ArrowDown"?-1:0;
   if(e.shiftKey&&direction){const k=keyboard.current??={anchor:current,cursor:current,frame,step};k.cursor=Math.max(k.frame.start,Math.min(k.frame.end,k.cursor+direction*k.step));const range=chartInterval(k.anchor,k.cursor,k.frame);setDraft(range?{key,range,frame:k.frame}:undefined);return false}
   if(e.key==="Enter"){e.preventDefault();const range=selected??chartInterval(Math.max(start,Math.min(current,end-step)),Math.min(end,Math.max(current+step,start+step)),frame);if(range)show(range,draft?.key===key?draft.frame:frame);return true}
   if(direction||e.key==="Home"||e.key==="End"){keyboard.current=undefined;setDraft(undefined)}
   return false;
  }
 };
}

import {useEffect,useRef,type RefObject} from 'react';

/** A short crossing grace period joins a portalled readout to its trigger. */
export function useReadoutHover(anchor:RefObject<Element|null>,id:string,onDismiss:()=>void){
 const timer=useRef<ReturnType<typeof setTimeout>|undefined>(undefined),close=useRef(onDismiss);close.current=onDismiss;
 const keep=()=>{clearTimeout(timer.current);timer.current=undefined};
 const dismiss=()=>{keep();close.current()};
 const leave=()=>{keep();timer.current=setTimeout(()=>{timer.current=undefined;if(!anchor.current?.contains(document.activeElement)&&!document.getElementById(id)?.contains(document.activeElement))close.current()},160)};
 const blur=(relatedTarget:EventTarget|null)=>{if(relatedTarget instanceof Node&&(anchor.current?.contains(relatedTarget)||document.getElementById(id)?.contains(relatedTarget)))return;dismiss()};
 useEffect(()=>()=>clearTimeout(timer.current),[]);
 return {keep,leave,blur,dismiss};
}

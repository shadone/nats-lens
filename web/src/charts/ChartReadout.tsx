import {useLayoutEffect, useRef, type ReactNode, type RefObject} from "react";
import {createPortal} from "react-dom";

/** A viewport overlay: plot width and table/card clipping do not size the reading. */
export function ChartReadout({anchor, fraction, id, contentKey, children}: {anchor:RefObject<Element|null>;fraction:number;id:string;contentKey:string;children:ReactNode}) {
  const ref=useRef<HTMLDivElement>(null);
  useLayoutEffect(()=>{
    const plot=anchor.current,tip=ref.current;
    if(!plot||!tip)return;
    let frame:number|undefined;
    const position=()=>{
      const view=window.visualViewport;
      const left=view?.offsetLeft??0,top=view?.offsetTop??0,width=view?.width??window.innerWidth,height=view?.height??window.innerHeight;
      const margin=parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--sp-3"))||12;
      const gap=parseFloat(getComputedStyle(document.documentElement).getPropertyValue("--sp-2"))||8;
      const r=plot.getBoundingClientRect();
      tip.style.maxWidth=`${Math.max(0,width-2*margin)}px`;
      tip.style.maxHeight=`${Math.max(0,height-2*margin)}px`;
      const t=tip.getBoundingClientRect();
      const x=Math.max(left+margin,Math.min(left+width-margin-t.width,r.left+r.width*fraction-t.width/2));
      const below=r.bottom+gap;
      const y=Math.max(top+margin,Math.min(top+height-margin-t.height,below+t.height<=top+height-margin?below:r.top-gap-t.height));
      tip.style.left=`${x}px`;tip.style.top=`${y}px`;
      tip.style.visibility=r.bottom<top||r.top>top+height||r.right<left||r.left>left+width?"hidden":"visible";
    };
    const schedule=()=>{if(frame===undefined)frame=requestAnimationFrame(()=>{frame=undefined;position()})};
    position();
    window.addEventListener("scroll",schedule,{capture:true,passive:true});window.addEventListener("resize",schedule);
    const view=window.visualViewport;view?.addEventListener("scroll",schedule);view?.addEventListener("resize",schedule);
    const observer=typeof ResizeObserver==="undefined"?undefined:new ResizeObserver(schedule);observer?.observe(plot);observer?.observe(tip);
    return ()=>{window.removeEventListener("scroll",schedule,true);window.removeEventListener("resize",schedule);view?.removeEventListener("scroll",schedule);view?.removeEventListener("resize",schedule);observer?.disconnect();if(frame!==undefined)cancelAnimationFrame(frame)};
  },[anchor,fraction,contentKey]);
  // Keep a chart inside a native modal in its accessible top-layer subtree.
  return createPortal(<div ref={ref} id={id} role="tooltip" className="chart-readout" style={{visibility:"hidden"}}>{children}</div>,anchor.current?.closest("dialog")??document.body);
}

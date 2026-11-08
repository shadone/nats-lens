import {useLayoutEffect, useRef, type ReactNode, type RefObject} from "react";
import {createPortal} from "react-dom";

/** A viewport overlay: plot width and table/card clipping do not size the reading. */
export function ChartReadout({anchor, fraction, id, contentKey, children,hover}: {anchor:RefObject<Element|null>;fraction:number;id:string;contentKey:string;children:ReactNode;hover?:{keep:()=>void;leave:()=>void;blur:(relatedTarget:EventTarget|null)=>void}}) {
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
      let clipLeft=left,clipRight=left+width,clipTop=top,clipBottom=top+height;
      for(let parent=plot.parentElement;parent;parent=parent.parentElement){
        const style=getComputedStyle(parent),bounds=parent.getBoundingClientRect();
        if(!bounds.width||!bounds.height)continue;
        if(/auto|scroll|hidden|clip/.test(style.overflowX||style.overflow)){clipLeft=Math.max(clipLeft,bounds.left);clipRight=Math.min(clipRight,bounds.right)}
        if(/auto|scroll|hidden|clip/.test(style.overflowY||style.overflow)){clipTop=Math.max(clipTop,bounds.top);clipBottom=Math.min(clipBottom,bounds.bottom)}
      }
      tip.style.visibility=!plot.isConnected||r.bottom<clipTop||r.top>clipBottom||r.right<clipLeft||r.left>clipRight?"hidden":"visible";
    };
    const schedule=()=>{if(frame===undefined)frame=requestAnimationFrame(()=>{frame=undefined;position()})};
    const keyboard=(e:KeyboardEvent)=>{
      if((e.key!=="PageDown"&&e.key!=="PageUp")||!plot.contains(document.activeElement)||tip.scrollHeight<=tip.clientHeight)return;
      e.preventDefault();tip.scrollBy({top:(e.key==="PageDown"?1:-1)*tip.clientHeight*.8,behavior:"auto"});
    };
    position();
    document.addEventListener("keydown",keyboard);
    window.addEventListener("scroll",schedule,{capture:true,passive:true});window.addEventListener("resize",schedule);
    const view=window.visualViewport;view?.addEventListener("scroll",schedule);view?.addEventListener("resize",schedule);
    const observer=typeof ResizeObserver==="undefined"?undefined:new ResizeObserver(schedule);observer?.observe(plot);observer?.observe(tip);
    return ()=>{document.removeEventListener("keydown",keyboard);window.removeEventListener("scroll",schedule,true);window.removeEventListener("resize",schedule);view?.removeEventListener("scroll",schedule);view?.removeEventListener("resize",schedule);observer?.disconnect();if(frame!==undefined)cancelAnimationFrame(frame)};
  },[anchor,fraction,contentKey]);
  // Keep a chart inside a native modal in its accessible top-layer subtree.
  return createPortal(<div ref={ref} id={id} role="tooltip" className="chart-readout" style={{visibility:"hidden"}} onPointerEnter={hover?.keep} onPointerLeave={hover?.leave} onPointerMove={e=>e.stopPropagation()} onPointerDown={e=>e.stopPropagation()} onFocusCapture={hover?.keep} onBlurCapture={e=>hover?.blur(e.relatedTarget)}>{children}</div>,anchor.current?.closest("dialog")??document.body);
}

import {useLayoutEffect} from "react";

// Native dialogs make the page inert, but do not lock its scroll container.
// Share one lock so closing a nested dialog cannot unlock its parent.
let holders=0;
let restore:(()=>void)|undefined;
function lock() {
  if(holders++===0) {
    const root=document.documentElement,body=document.body;
    const x=window.scrollX,y=window.scrollY;
    const properties=["position","top","left","width","overflow","padding-right","box-sizing"];
    const saved=properties.map(key=>[key,body.style.getPropertyValue(key),body.style.getPropertyPriority(key)]);
    const overflow=root.style.getPropertyValue("overflow"),priority=root.style.getPropertyPriority("overflow");
    const gutter=Math.max(0,window.innerWidth-root.clientWidth);
    const padding=parseFloat(getComputedStyle(body).paddingRight)||0;
    root.style.setProperty("overflow","hidden");
    Object.assign(body.style,{position:"fixed",top:`${-y}px`,left:`${-x}px`,width:"100%",overflow:"hidden",boxSizing:"border-box"});
    if(gutter)body.style.paddingRight=`${padding+gutter}px`;
    restore=()=>{
      for(const [key,value,important] of saved)if(value)body.style.setProperty(key,value,important);else body.style.removeProperty(key);
      if(overflow)root.style.setProperty("overflow",overflow,priority);else root.style.removeProperty("overflow");
      window.scrollTo(x,y);
    };
  }
  return ()=>{if(--holders===0){restore?.();restore=undefined}};
}
export function useModalScrollLock(open:boolean) {
  useLayoutEffect(()=>open?lock():undefined,[open]);
}

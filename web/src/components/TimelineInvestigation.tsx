import {createContext,useCallback,useContext,useLayoutEffect,useState,type ReactNode} from "react";
import {createPortal} from "react-dom";
import {useSystem} from "../lib/useSystem";
import {Dialog} from "./Dialog";
export {TimelineFindingEpisodes as TimelineFindingRange} from "./TimelineFindingEpisodes";

type Selection={label:string;preview:ReactNode;details?:ReactNode};
const Context=createContext<((value:Selection)=>void)|null>(null);
export const useTimelineInvestigation=()=>useContext(Context);

/** Selection belongs to the Timeline, not a mark whose key changes on refresh. */
export function TimelineInvestigationProvider({children}:{children:ReactNode}) {
 const system=useSystem();
 const [stored,setSelection]=useState<{system:string;value:Selection}>();
 const selection=stored?.system===system?stored.value:undefined;
 useLayoutEffect(()=>setSelection(undefined),[system]);
 const select=useCallback((value:Selection)=>setSelection({system,value}),[system]);
 return <Context.Provider value={select}>{children}{selection&&createPortal(<Dialog fullScreen open title={selection.label} onClose={()=>setSelection(undefined)}><div className="timeline-details"><p className="muted-note">Timeline context is kept as selected and does not live-update while this view is open. Any finding list below uses the fixed selected interval.</p>{selection.preview}{selection.details}</div></Dialog>,document.body)}</Context.Provider>;
}


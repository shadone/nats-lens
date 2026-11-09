import {createContext,useCallback,useContext,useState,type ReactNode} from "react";
import {createPortal} from "react-dom";
import {Dialog} from "./Dialog";
export {TimelineFindingEpisodes as TimelineFindingRange} from "./TimelineFindingEpisodes";

type Selection={label:string;preview:ReactNode;details?:ReactNode};
const Context=createContext<((value:Selection)=>void)|null>(null);
export const useTimelineInvestigation=()=>useContext(Context);

/** Selection belongs to the Timeline, not a mark whose key changes on refresh. */
export function TimelineInvestigationProvider({children}:{children:ReactNode}) {
 const [selection,setSelection]=useState<Selection>();
 const select=useCallback((value:Selection)=>setSelection(value),[]);
 return <Context.Provider value={select}>{children}{selection&&createPortal(<Dialog fullScreen open title={selection.label} onClose={()=>setSelection(undefined)}><div className="timeline-details"><p className="muted-note">Timeline context is kept as selected and does not live-update while this view is open. Any finding list below uses the fixed selected interval.</p>{selection.preview}{selection.details}</div></Dialog>,document.body)}</Context.Provider>;
}


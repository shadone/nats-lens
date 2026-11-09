import {useQuery} from '@tanstack/react-query';
import {durationFromMs} from '@bufbuild/protobuf/wkt';
import {lens} from './client';
import {event, timeline} from './adapt-findings';
import {window as adaptWindow} from './adapt-common';
import {eventReadId,frozenEventRange,verifyEventSelection} from './eventIdentity';
import {getEvent,systemInit} from './requests';
import {useSystem} from '../lib/useSystem';
import {bucketFor} from '../lib/timeRange';
import type {FrozenEvidenceRange} from './model';

export async function selectedTimelineEvents(system:string,_range:FrozenEvidenceRange,ids:string[],boundary?:string,signal?:AbortSignal){
 if(ids.length===0)return {items:[],totalCount:0,nextPageToken:''};
 if(ids.length>50||new Set(ids).size!==ids.length||ids.some(x=>eventReadId(x)===0n))throw new Error('Select at most 50 unique recorded event identities per page.');
 const r=await lens.getEventBatch({...systemInit(system),ids,eventReadId:eventReadId(boundary)},{signal});
 verifyEventSelection(r,ids,boundary);
 if(r.totalCount!==r.events.length)throw new Error("The source returned an incomplete exact event batch.");
 return {items:r.events.map(event),totalCount:r.totalCount,nextPageToken:''};
}
export function useSelectedTimelineEvents(range:FrozenEvidenceRange,ids:string[],boundary?:string){
 const system=useSystem();return useQuery({queryKey:['timeline-events',system,range,ids,boundary],queryFn:({signal})=>selectedTimelineEvents(system,range,ids,boundary,signal),enabled:ids.length>0,staleTime:Infinity,refetchOnWindowFocus:false,refetchOnReconnect:false});
}
export function useCapturedEvent(id:string|undefined,boundary?:string){
 const system=useSystem();return useQuery({queryKey:['event',system,id,boundary],queryFn:({signal})=>getEvent(system,id!,signal,boundary),enabled:id!==undefined,staleTime:Infinity,refetchOnWindowFocus:false,refetchOnReconnect:false});
}
export function useTimelineWindow(windowId:string,range:FrozenEvidenceRange,boundary:string){
 const system=useSystem();return useQuery({queryKey:['timeline-window',system,windowId,range,boundary],queryFn:async({signal})=>{
  const r=await lens.getStatusTimeline({...systemInit(system),range:frozenEventRange(range),eventReadId:eventReadId(boundary),bucket:durationFromMs(bucketFor(Date.parse(range.end)-Date.parse(range.start)))},{signal});
  if(r.eventReadId!==eventReadId(boundary))throw new Error('This server did not acknowledge the captured Timeline event boundary.');
  return {window:r.windows.find(w=>w.id===windowId)?adaptWindow(r.windows.find(w=>w.id===windowId)!):undefined,timeline:timeline(r)};
 },staleTime:Infinity,refetchOnWindowFocus:false,refetchOnReconnect:false});
}

export function useCapturedSelectionPage(_selection:import('../lib/capturedEventSelection').CapturedSelection,_offset:number,_pageSize:number){
 return useQuery({queryKey:['captured-selection-stub'],queryFn:async()=>({items:[] as import('./model').LensEvent[],totalCount:0})});
}

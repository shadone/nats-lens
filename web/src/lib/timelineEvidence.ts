import type {FrozenEvidenceRange,LaneEvent,Window} from '../api/model';
import {isoSecond} from './format';
export const timelineTime=(time:number)=>`${isoSecond(time)} UTC`;
export const eventSourceLabel=(source:LaneEvent['source'])=>source==='kubernetes'?'Kubernetes':source==='finding'?'Finding':source==='collector'?'Collector':source==='nats'?'NATS':'Unknown source';
export function evidenceRange(from:number,end:number):FrozenEvidenceRange{return {start:new Date(from).toISOString(),end:new Date(end).toISOString()}}
export function evidenceQuery(system:string,range:FrozenEvidenceRange,account=''){
 const p=new URLSearchParams({system,from:range.start,to:range.end});if(account)p.set('account',account);return p;
}
export function recordedEventPath(id:string,system:string,range:FrozenEvidenceRange,readId?:string){
 const p=evidenceQuery(system,range);p.set('id',id);if(readId!==undefined)p.set('eventReadId',readId);return `/events?${p}`;
}
export function windowEvidencePath(w:Window,system:string){
 if(!w.id||!w.captureRange||w.eventReadId===undefined)return undefined;
 const p=evidenceQuery(system,w.captureRange);p.set('eventReadId',w.eventReadId);return `/timeline/windows/${encodeURIComponent(w.id)}?${p}`;
}
export interface TimelineEventGroup {key:string;events:LaneEvent[];first:number;last:number;center:number}
/** Group colliding 32px hit regions, not causal incidents. Without a measured
 * width only equal timestamps group. All captured members remain available. */
export function groupTimelineEvents(events:LaneEvent[],from:number,end:number,width:number):TimelineEventGroup[]{
 if(!(end>from))return [];
 const sorted=events.filter(e=>Number.isFinite(e.time)&&e.time>=from&&e.time<=end).slice().sort((a,b)=>a.time-b.time||a.id.localeCompare(b.id));
 const gap=width>0?(end-from)*32/width:0;const groups:TimelineEventGroup[]=[];
 for(const e of sorted){const last=groups[groups.length-1];if(last&&e.time-last.last<=gap){last.events.push(e);last.last=e.time;last.center=(last.first+last.last)/2;last.key+=`|${e.id}`}else groups.push({key:e.id,events:[e],first:e.time,last:e.time,center:e.time})}
 return groups;
}

import {useQuery} from '@tanstack/react-query';
import type {Entity} from './model';
import {useSystem} from '../lib/useSystem';
import {rangeKey,resolveRange,type RangeSpec} from '../lib/timeRange';
import type {ResourceMetric} from '../lib/resourceMetrics';
import {lens} from './client';
import {entity,ms} from './adapt-common';
import {entityInit,rangeInit,systemInit} from './requests';
import {trendSeries,type TrendSeries} from './overview';
export const RESOURCE_METRIC_PAGE_SIZE=12;
export interface ResourceMetricRow {entity:Entity;series?:TrendSeries;error:string}
export interface ResourceMetricComparison {system:string;start:number;end:number;readAt?:number;rows:ResourceMetricRow[];total:number;next:string;epoch:string}
export async function getResourceMetricComparison(system:string,spec:RangeSpec,family:ResourceMetric,selection:Entity|undefined,account:string,stream:string,token:string,signal?:AbortSignal):Promise<ResourceMetricComparison>{
 const info=await lens.getLensInfo(systemInit(system),{signal});const rawClock=ms(info.now);const readAt=rawClock!==undefined&&Number.isFinite(rawClock)?rawClock:undefined;
 if(readAt===undefined&&spec.kind==='live'&&!token)throw new Error('Source clock unavailable; cannot resolve the live comparison');
 const resolvedSystem=system||info.system;let {start,end}=resolveRange(spec,readAt??0);let range=rangeInit({kind:'fixed',from:start,to:end});
 let entities:Entity[]=[],total=1,next='',epoch='';
 if(selection){if(selection.kind!==family.kind)throw new Error('Selected resource kind does not match this metric');entities=[selection]}
 else{
  const common={...systemInit(resolvedSystem),range:token?undefined:range,account,limit:RESOURCE_METRIC_PAGE_SIZE,pageToken:token};
  const response=family.kind==='stream'?await lens.listStreams({...common,includeKv:true,summaryOnly:true},{signal}):await lens.listConsumers({...common,stream,includeEphemeral:true},{signal});
  const a=ms(response.range?.start),b=ms(response.range?.end);if(a===undefined||b===undefined||!Number.isFinite(a)||!Number.isFinite(b)||a>=b)throw new Error('Resource page omitted valid pinned bounds');
  start=a;end=b;range=rangeInit({kind:'fixed',from:start,to:end});total=response.totalCount;next=response.nextPageToken;epoch=String(response.epoch);
  const summaries='streams' in response?response.streams:response.consumers;if(summaries.length>RESOURCE_METRIC_PAGE_SIZE)throw new Error('Resource page exceeds the history work bound');
  entities=summaries.map(row=>{const e=entity(row.entity);if(!e||e.kind!==family.kind||!e.key||!e.account||!e.stream||(family.kind==='consumer'&&!e.consumer)||account&&e.account!==account||family.kind==='consumer'&&stream&&e.stream!==stream)throw new Error('Resource page identity does not match the requested scope');return e});
  if(new Set(entities.map(e=>e.key)).size!==entities.length)throw new Error('Resource page contains duplicate identities');
 }
 if(!entities.length)return {system:resolvedSystem,start,end,readAt,rows:[],total,next,epoch};
 const batch=await lens.getTimeSeriesBatch({...systemInit(resolvedSystem),range,maxPoints:96,queries:entities.map(e=>({metric:family.metric,entity:entityInit(e),includeObservations:true}))},{signal});
 if(batch.results.length!==entities.length)throw new Error('Incomplete resource metric response');
 const rows=entities.map((e,i)=>{const r=batch.results[i];if(r.series&&!r.error&&(r.series.metric!==family.metric||r.series.entity?.key!==e.key||r.series.entity.type!==entityInit(e)?.type))throw new Error('Metric response identity does not match the requested resource');return {entity:e,series:r.error?undefined:trendSeries(r.series),error:r.error||''}});
 return {system:resolvedSystem,start,end,readAt,rows,total,next,epoch};
}
export function useResourceMetricComparison(spec:RangeSpec,family:ResourceMetric,selection:Entity|undefined,account:string,stream:string,token:string){const system=useSystem();return useQuery({queryKey:['resource-metric-comparison',system,rangeKey(spec),family.key,selection?.key,account,stream,token],queryFn:({signal})=>getResourceMetricComparison(system,spec,family,selection,account,stream,token,signal)})}

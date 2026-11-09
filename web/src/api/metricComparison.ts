import {useQuery} from '@tanstack/react-query';
import type {RangeSpec} from '../lib/timeRange';
import {LIVE_REFRESH_MS,rangeKey,resolveRange} from '../lib/timeRange';
import type {MetricFamily} from '../lib/metricNavigation';
import {useSystem} from '../lib/useSystem';
import {lens} from './client';
import {ms} from './adapt-common';
import {serverList,type ServerRow} from './servers';
import {rangeInit,systemInit} from './requests';
import {trendSeries,type TrendSeries} from './overview';
import {EntityType} from '../gen/natslens/v1/common_pb';
import type {Entity} from './model';
export const METRIC_PAGE_SIZE=12;
export interface MetricComparisonRow {name:string;metadata?:ServerRow;series:(TrendSeries|undefined)[];errors:string[]}
export interface MetricComparison {system:string;start:number;end:number;readAt?:number;rows:MetricComparisonRow[];fleet:(TrendSeries|undefined)[];fleetErrors:string[];total:number;page:number;pages:number}
const serverEntity=(name:string):Entity=>({kind:'server',key:name,server:name,name,account:'',stream:'',consumer:''});
export async function getMetricComparison(system:string,spec:RangeSpec,family:MetricFamily,server:string,filter:string,page:number,signal?:AbortSignal):Promise<MetricComparison>{
 // Always obtain a fresh provider clock before resolving a live comparison.
 const info=await lens.getLensInfo(systemInit(system),{signal});const rawClock=ms(info.now);const readAt=rawClock!==undefined&&Number.isFinite(rawClock)?rawClock:undefined;if((readAt===undefined||!Number.isFinite(readAt))&&spec.kind==='live')throw new Error('Source clock unavailable; cannot resolve the live comparison');
 const resolvedSystem=system||info.system||'';
 const {start,end}=resolveRange(spec,readAt??0);const range=rangeInit({kind:'fixed',from:start,to:end});const list=serverList(await lens.listServers({...systemInit(resolvedSystem),range},{signal}));
 const byName=new Map(list.servers.map(s=>[s.name,s]));const names=[...new Set([...byName.keys(),...list.expected])].filter(name=>(!server||name===server)&&(!filter||name.toLowerCase().includes(filter.toLowerCase()))).sort((a,b)=>a<b?-1:a>b?1:0);
 const pages=Math.max(1,Math.ceil(names.length/METRIC_PAGE_SIZE)),effectivePage=Math.max(0,Math.min(pages-1,Number.isSafeInteger(page)?page:0));const selected=names.slice(effectivePage*METRIC_PAGE_SIZE,(effectivePage+1)*METRIC_PAGE_SIZE);
 const queries=[...family.fleetMetrics.map(metric=>({metric,entity:undefined,includeObservations:true})),...selected.filter(name=>byName.has(name)).flatMap(name=>family.metrics.map(metric=>({metric,entity:{type:EntityType.SERVER,key:name,name,server:name},includeObservations:true})))];
 if(queries.length>32)throw new Error('Comparison exceeds the series batch limit');
 const batch=await lens.getTimeSeriesBatch({...systemInit(resolvedSystem),range,maxPoints:96,queries},{signal});if(batch.results.length!==queries.length)throw new Error('Incomplete metric comparison response');
 for(let i=0;i<queries.length;i++){const r=batch.results[i],q=queries[i];if(r.error||!r.series)continue;if(r.series.metric!==q.metric||(q.entity&&(r.series.entity?.type!==q.entity.type||r.series.entity?.key!==q.entity.key)))throw new Error('Metric response identity does not match the requested series')}
 let offset=family.fleetMetrics.length;const rows=selected.map(name=>{const metadata=byName.get(name);const values=metadata?batch.results.slice(offset,offset+family.metrics.length):[];if(metadata)offset+=family.metrics.length;return {name,metadata,series:family.metrics.map((_,i)=>values[i]?.error?undefined:trendSeries(values[i]?.series)),errors:family.metrics.map((_,i)=>metadata?(values[i]?.error??''):'No server process was observed in the selected range')}});
 return {system:resolvedSystem,start,end,readAt,rows,fleet:family.fleetMetrics.map((_,i)=>batch.results[i].error?undefined:trendSeries(batch.results[i].series)),fleetErrors:family.fleetMetrics.map((_,i)=>batch.results[i].error),total:names.length,page:effectivePage,pages};
}
export function useMetricComparison(spec:RangeSpec,family:MetricFamily,server:string,filter:string,page:number){const system=useSystem();return useQuery({queryKey:['metric-comparison',system,rangeKey(spec),family.key,server,filter,page],queryFn:({signal})=>getMetricComparison(system,spec,family,server,filter,page,signal),refetchInterval:spec.kind==='live'?LIVE_REFRESH_MS:false})}
export {serverEntity};

import {useQuery} from '@tanstack/react-query';
import type {AggregateMetric} from '../lib/aggregateMetrics';
import {LIVE_REFRESH_MS,rangeKey,resolveRange,type RangeSpec} from '../lib/timeRange';
import {useSystem} from '../lib/useSystem';
import {lens} from './client';
import {ms} from './adapt-common';
import {systemInit,rangeInit} from './requests';
import {trendSeries,type TrendSeries} from './overview';
import {EntityType} from '../gen/natslens/v1/common_pb';
export interface AggregateComparison {system:string;start:number;end:number;readAt?:number;series:(TrendSeries|undefined)[];errors:string[]}
export async function getAggregateComparison(system:string,spec:RangeSpec,family:AggregateMetric,signal?:AbortSignal):Promise<AggregateComparison>{
 if(!family.metrics.length||family.metrics.length>2)throw new Error('Aggregate comparison exceeds its two-series bound');
 const info=await lens.getLensInfo(systemInit(system),{signal});const raw=ms(info.now),readAt=raw!==undefined&&Number.isFinite(raw)?raw:undefined;
 if(readAt===undefined&&spec.kind==='live')throw new Error('Source clock unavailable; cannot resolve a live aggregate interval');
 const resolvedSystem=system||info.system;const {start,end}=resolveRange(spec,readAt??0);
 const response=await lens.getTimeSeriesBatch({...systemInit(resolvedSystem),range:rangeInit({kind:'fixed',from:start,to:end}),maxPoints:96,queries:family.metrics.map(metric=>({metric,includeObservations:true}))},{signal});
 if(response.results.length!==family.metrics.length)throw new Error('Incomplete aggregate comparison response');
 response.results.forEach((r,i)=>{if(r.series&&!r.error&&(r.series.metric!==family.metrics[i]||r.series.entity&&![EntityType.SYSTEM,EntityType.UNSPECIFIED].includes(r.series.entity.type)))throw new Error('Aggregate response identity does not match the requested metric')});
 return {system:resolvedSystem,start,end,readAt,series:response.results.map(r=>r.error?undefined:trendSeries(r.series)),errors:response.results.map(r=>r.error||'')};
}
export function useAggregateComparison(spec:RangeSpec,family:AggregateMetric){const system=useSystem();return useQuery({queryKey:['aggregate-metric-comparison',system,rangeKey(spec),family.key],queryFn:({signal})=>getAggregateComparison(system,spec,family,signal),refetchInterval:spec.kind==='live'?LIVE_REFRESH_MS:false})}

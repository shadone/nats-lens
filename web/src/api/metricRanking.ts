import {create} from '@bufbuild/protobuf';
import {useQuery} from '@tanstack/react-query';
import {lens} from './client';
import {entity,ms,unit} from './adapt-common';
import {rangeInit,systemInit} from './requests';
import {trendSeries,type MetricObservations} from './overview';
import type {Entity,Unit} from './model';
import {SeriesSchema} from '../gen/natslens/v1/timeseries_pb';
import {MetricRanking} from '../gen/natslens/v1/metric_contributors_pb';
import {useSystem} from '../lib/useSystem';
import {LIVE_REFRESH_MS,rangeKey,resolveRange,type RangeSpec} from '../lib/timeRange';
export type MetricRankingKind='server'|'stream'|'consumer';
export interface MetricRankingScope{account?:string;stream?:string;server?:string;query?:string}
export interface GlobalMetricRanking {system:string;start:number;end:number;readAt?:number;unit:Unit;rows:{entity:Entity;observations?:MetricObservations}[];total:number;next:string}
export async function getMetricRanking(system:string,spec:RangeSpec,metric:string,kind:MetricRankingKind,ranking:MetricRanking,descending:boolean,scope:MetricRankingScope,token:string,signal?:AbortSignal):Promise<GlobalMetricRanking>{
 let range;let expected:{start:number;end:number}|undefined;if(!token){let now=0;if(spec.kind==='live'){const info=await lens.getLensInfo(systemInit(system),{signal});const clock=ms(info.now);if(clock===undefined||!Number.isFinite(clock))throw new Error('Source clock unavailable for metric ranking');now=clock;system=system||info.system}const resolved=resolveRange(spec,now);expected=resolved;range=rangeInit({kind:'fixed',from:resolved.start,to:resolved.end})}
 const response=await lens.listMetricContributors({...systemInit(system),metric,range,ranking,descending,...scope,limit:12,pageToken:token},{signal});
 if(response.metric!==metric)throw new Error('Ranking metric does not match the request');
 const start=ms(response.range?.start),end=ms(response.range?.end);if(start===undefined||end===undefined||!Number.isFinite(start)||!Number.isFinite(end)||start>=end)throw new Error('Ranking omitted valid fixed bounds');
 if(expected&&(start!==expected.start||end!==expected.end))throw new Error('Ranking changed requested bounds');
 if(response.contributors.length>12||response.totalCount<response.contributors.length)throw new Error('Ranking page exceeds its bound');
 const rows=response.contributors.map(row=>{const e=entity(row.entity);if(!e||e.kind!==kind||!e.key||e.key!==(kind==='server'?e.server:`${e.account}/${e.stream}${kind==='consumer'?`/${e.consumer}`:''}`)||kind!=='server'&&(!e.account||!e.stream||kind==='consumer'&&!e.consumer)||scope.account&&e.account!==scope.account||scope.stream&&e.stream!==scope.stream||scope.server&&e.server!==scope.server)throw new Error('Ranking identity does not match the requested scope');return {entity:e,observations:trendSeries(create(SeriesSchema,{metric,entity:row.entity,observations:row.observations}))?.observations}});
 if(new Set(rows.map(r=>r.entity.key)).size!==rows.length)throw new Error('Ranking contains duplicate identities');
 if(response.ranking!==ranking||response.descending!==descending)throw new Error('Ranking order does not match the request');
 return {system,start,end,readAt:ms(response.readAt),unit:unit(response.unit),rows,total:response.totalCount,next:response.nextPageToken};
}
export function useMetricRanking(spec:RangeSpec,metric:string,kind:MetricRankingKind,ranking:MetricRanking,descending:boolean,scope:MetricRankingScope,token:string,enabled:boolean){const system=useSystem();return useQuery({queryKey:['metric-ranking',system,rangeKey(spec),metric,kind,ranking,descending,scope,token],queryFn:({signal})=>getMetricRanking(system,spec,metric,kind,ranking,descending,scope,token,signal),enabled,refetchInterval:enabled&&spec.kind==='live'&&!token?LIVE_REFRESH_MS:false})}

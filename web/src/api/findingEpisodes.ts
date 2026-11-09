import {useQuery} from '@tanstack/react-query';
import {lens} from './client';
import {entityInit,rangeInit,systemInit} from './requests';
import {finding,transition} from './adapt-findings';
import {ms,severity} from './adapt-common';
import type {Entity,FindingState} from './model';
import {FindingState as State} from '../gen/natslens/v1/findings_pb';
import {useSystem} from '../lib/useSystem';

const states:Record<State,FindingState>={[State.UNSPECIFIED]:'unknown',[State.FIRING]:'firing',[State.CHANGED]:'changed',[State.FLAPPING]:'flapping',[State.RESOLVED]:'resolved'};
export interface EpisodeOptions {checkCode?:string;entity?:Entity;findingId?:string;firstEpoch?:string;readEpoch?:string;pageSize?:number;pageToken?:string;includeTransitions?:boolean;transitionOffset?:number}
export function epoch(raw:string):bigint {
 if(!/^(0|[1-9]\d{0,18})$/.test(raw)||BigInt(raw)>9223372036854775807n)throw new Error('The finding-history boundary is invalid.');
 return BigInt(raw);
}
export async function listFindingEpisodes(system:string,from:number,end:number,o:EpisodeOptions,signal?:AbortSignal){
 const r=await lens.listFindingEpisodes({...systemInit(system),range:rangeInit({kind:'fixed',from,to:end}),checkCode:o.checkCode,entity:entityInit(o.entity),findingId:o.findingId,firstRetainedEpoch:o.firstEpoch===undefined?undefined:epoch(o.firstEpoch),readEpoch:o.readEpoch===undefined?undefined:epoch(o.readEpoch),pageSize:o.pageSize??50,pageToken:o.pageToken,includeTransitions:o.includeTransitions,transitionOffset:o.transitionOffset},{signal});
 return {items:r.episodes.filter(e=>e.finding!==undefined).map(e=>({finding:finding(e.finding!),firstEpoch:e.firstRetainedEpoch.toString(),startEpoch:e.startEpoch?.toString(),firstAt:ms(e.firstRetainedAt),resolutionAt:ms(e.observedResolutionAt),resolutionEpoch:e.resolutionEpoch.toString(),latestState:states[e.latestRetainedState]??'unknown',latestSeverity:severity(e.latestRetainedSeverity),latestStartEpoch:e.latestStartEpoch.toString(),latestAt:ms(e.latestObservedAt),transitions:e.transitions.map(t=>({...transition(t),epoch:t.epoch.toString()})),transitionCount:e.transitionCount,nextTransitionOffset:e.nextTransitionOffset,transitionToken:e.transitionPageToken,unavailable:e.unavailable,causeEventId:e.causeEventId.toString()})),totalCount:r.totalCount,nextPageToken:r.nextPageToken,readEpoch:r.readEpoch.toString(),readAt:ms(r.readAt),unavailable:r.unavailable};
}
export type EpisodePage=Awaited<ReturnType<typeof listFindingEpisodes>>;
export type FindingEpisode=EpisodePage['items'][number];
/** Fixed selected evidence never follows the live clock or borrows another scope. */
export function useFindingEpisodes(from:number,end:number,o:EpisodeOptions){
 const system=useSystem();
 return useQuery({queryKey:['finding-episodes',system,from,end,o],queryFn:({signal})=>listFindingEpisodes(system,from,end,o,signal),refetchOnWindowFocus:false,refetchOnReconnect:false,staleTime:Infinity});
}

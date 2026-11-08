import type {Entity,Unit} from '../api/model';
export interface ResourceMetric {key:string;metric:string;kind:'stream'|'consumer';title:string;unit:Unit;summary:'count'|'gauge'|'rate';explanation:string}
export const resourceMetrics:ResourceMetric[]=[];
export function resourceMetric(_key:string):ResourceMetric|undefined{return undefined}
export function resourceMetricDestination(_metric:string,_entity:Entity|undefined):string|undefined{return undefined}
export function resourceSelection(_family:ResourceMetric,_params:URLSearchParams):Entity|undefined{return undefined}

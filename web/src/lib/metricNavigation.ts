import type {InvestigationSelection} from './investigationContext';
export type MetricFamilyKey='cpu'|'memory'|'connections'|'messages'|'bytes';
export interface MetricFamily {key:MetricFamilyKey;title:string;metrics:string[];fleetMetrics:string[];labels:string[];explanation:string;summary:'gauge'|'count'|'rate'}
export const metricFamilies:MetricFamily[]=[];
export function metricFamily(key:string):MetricFamily|undefined{return metricFamilies.find(f=>f.key===key)}
export function metricDestination(_selection:InvestigationSelection|undefined):string|undefined{return undefined}

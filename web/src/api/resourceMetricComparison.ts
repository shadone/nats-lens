import type {Entity} from './model';
import type {RangeSpec} from '../lib/timeRange';
import type {ResourceMetric} from '../lib/resourceMetrics';
import type {TrendSeries} from './overview';
export interface ResourceMetricRow {entity:Entity;series?:TrendSeries;error:string}
export interface ResourceMetricComparison {system:string;start:number;end:number;readAt?:number;rows:ResourceMetricRow[];total:number;next:string;epoch:string}
export async function getResourceMetricComparison(_system:string,_spec:RangeSpec,_family:ResourceMetric,_selection:Entity|undefined,_account:string,_stream:string,_token:string,_signal?:AbortSignal):Promise<ResourceMetricComparison>{throw new Error('Resource metric comparison is not implemented')}

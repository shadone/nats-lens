import type {AggregateMetric} from '../lib/aggregateMetrics';
import type {RangeSpec} from '../lib/timeRange';
import type {TrendSeries} from './overview';
export interface AggregateComparison {system:string;start:number;end:number;readAt?:number;series:(TrendSeries|undefined)[];errors:string[]}
export async function getAggregateComparison(_system:string,_spec:RangeSpec,_family:AggregateMetric,_signal?:AbortSignal):Promise<AggregateComparison>{throw new Error('Aggregate observations are not implemented')}

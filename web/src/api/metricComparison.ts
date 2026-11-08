import type {RangeSpec} from '../lib/timeRange';
import type {MetricFamily} from '../lib/metricNavigation';
export const METRIC_PAGE_SIZE=12;
export async function getMetricComparison(_system:string,_spec:RangeSpec,_family:MetricFamily,_server:string,_filter:string,_page:number,_signal?:AbortSignal):Promise<unknown>{throw new Error('Metric comparison not implemented')}

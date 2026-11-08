import type {MetricObservations} from '../api/overview';
const when=(at:number|undefined)=>at===undefined?'Unknown':new Date(at).toISOString().replace('.000Z','Z');
/** Preserve the exact decimal total; never multiply a sampled rate by time. */
export function TrafficCounterEvidence({observations}:{observations?:MetricObservations}){
 const c=observations?.counterComparisons;if(!c)return <span>Counter evidence unavailable from this source</span>;
 const known=c.observedIncrease!==undefined&&/^(0|[1-9]\d*)$/.test(c.observedIncrease)&&c.comparisonCount>0n;
 return <><strong>{known?BigInt(c.observedIncrease!).toLocaleString():'Unknown'}</strong><small>{c.comparisonCount.toLocaleString()} comparable report {c.comparisonCount===1n?'pair':'pairs'} · {c.serverIdCount.toLocaleString()} server IDs</small><details><summary>Comparison evidence</summary><p>{c.excludedReports.toLocaleString()} returned counter rows skipped, including {c.boundaryReports.toLocaleString()} crossing the range start.</p><p>{when(c.firstComparedAt)} to {when(c.lastComparedAt)} is the comparison envelope, not continuous coverage. Entirely absent reports cannot be counted as skipped rows. Missing values, resets, invalid timing and excessive gaps are not fabricated as traffic.</p></details></>;
}

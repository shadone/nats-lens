import {useEffect,useState} from 'react';
import {Link,useLocation,useSearchParams} from 'react-router';
import {useAggregateComparison,type AggregateComparison} from '../api/aggregateMetricComparison';
import type {AggregateMetric} from '../lib/aggregateMetrics';
import {aggregateMetrics} from '../lib/aggregateMetrics';
import {useTimeRange} from '../lib/useTimeRange';
import {globalQuery} from '../lib/globalQuery';
import {fmtValue} from '../lib/units';
import {bucketLabel} from '../lib/timeRange';
import {QueryView,RLink} from '../components/common';
import {TrendChart} from '../components/TrendChart';
import {useReportStale} from '../components/StaleContext';
import {Dialog} from '../components/Dialog';
import {FindingIntervalEvidence} from '../components/FindingIntervalEvidence';
const stamp=(at:number|undefined)=>at===undefined||!Number.isFinite(at)?'Unknown':new Date(at).toISOString().replace('.000Z','Z');
const count=(v:number|undefined)=>v===undefined||!Number.isFinite(v)?'Unknown':fmtValue(v,'count');
function total(value:string|undefined){if(!value||! /^-?\d+$/.test(value))return 'Unknown';try{return BigInt(value).toLocaleString()}catch{return 'Unknown'}}
function fixedLink(path:string,d:AggregateComparison){const [base,query]=path.split('?');const p=new URLSearchParams(globalQuery({kind:'fixed',from:d.start,to:d.end},d.system,''));new URLSearchParams(query).forEach((v,k)=>p.set(k,v));return `${base}?${p}`}
export function AggregateMetricInvestigation({family:f}:{family:AggregateMetric}){
 const {spec}=useTimeRange();const location=useLocation();const [params]=useSearchParams();const q=useAggregateComparison(spec,f);useReportStale(q);const [evidence,setEvidence]=useState<AggregateComparison>();
 useEffect(()=>setEvidence(undefined),[location.pathname,location.search]);
 return <div className="metric-page"><nav className="crumbs" aria-label="Breadcrumb"><RLink to="/metrics">Metric investigations</RLink></nav><h1>{f.title} investigation</h1><p>{f.explanation}</p><p>All accounts in this system. {params.get('account')&&`Account ${params.get('account')} remains navigation context; it does not filter these system aggregates.`}</p>
 <nav className="metric-family-links" aria-label="Aggregate metric investigations">{aggregateMetrics.map(x=><RLink key={x.key} to={`/metrics/${x.key}`} aria-current={x.key===f.key?'page':undefined}>{x.title}</RLink>)}</nav>
 <button className="btn" disabled={q.isFetching} onClick={()=>void q.refetch()}>Refresh observations</button>
 <QueryView q={q} what="aggregate metric observations">{d=><><p className="metric-window">Requested interval: {stamp(d.start)} to {stamp(d.end)}. Source clock: {stamp(d.readAt)}. {spec.kind==='live'?'Live observations refresh automatically.':'This interval is fixed.'}{q.isFetching&&' Refreshing; showing the last completed comparison.'}</p>
 <div className="metric-charts">{f.metrics.map((metric,i)=>{const s=d.series[i];return <TrendChart key={metric} metricNavigation={false} intervalActions title={f.labels[i]} investigation={{metric}} start={d.start} end={d.end} stepMs={s?.stepMs} bucketOverlap contiguousBuckets fmt={count} now={s?.points.length?`${count(s.points.at(-1)?.value)} · last bucket`:undefined} bucketNote={f.events?'Sum of observed transitions in bucket':'Last count in bucket'} summary={s?`${bucketLabel(s.stepMs??0)} buckets; unknown samples can occur inside populated buckets`:undefined} layers={[f.events?{kind:'bars',label:f.labels[i],style:{tone:'accent'},points:s?.points??[]}:{kind:'line',label:f.labels[i],style:{tone:'accent'},points:s?.points??[]}]} error={d.errors[i]}/>})}</div>
 <p>Charts show bucket aggregates. The table uses captured metric values before bucketing. Missing values stay unknown; known / returned samples are metric-query rows, not expected collections or proof of complete capture.</p>
 <div className="table-wrap" role="region" tabIndex={0} aria-label="Aggregate observation table"><table className="tbl metric-table" aria-label="Aggregate observations"><thead><tr><th scope="col">Measurement</th><th scope="col">Latest observed</th>{f.events?<th scope="col">Observed total</th>:<th scope="col">Observed minimum</th>}<th scope="col">{f.events?'Most changes in one collection':'Observed peak'}</th><th scope="col">Known / returned</th><th scope="col">Returned observation bounds</th></tr></thead><tbody>{f.metrics.map((metric,i)=>{const o=d.series[i]?.observations;return <tr key={metric}><th scope="row">{f.labels[i]}</th><td>{count(o?.latest)}<small>{stamp(o?.lastSampleAt)}</small></td><td>{f.events?total(o?.observedTotal):count(o?.minimum)}</td><td>{count(o?.maximum)}<small>{stamp(o?.maximumAt)}</small></td><td>{o?`${o.knownCount.toLocaleString()} / ${o.sampleCount.toLocaleString()}`:'Summary unavailable'}{d.errors[i]&&<p>{d.errors[i]}</p>}</td><td>{stamp(o?.firstSampleAt)}<small>to {stamp(o?.lastSampleAt)}</small></td></tr>})}</tbody></table></div>
 <section><h2>Inspect the supporting evidence</h2><p>An aggregate change does not identify an affected resource or establish a cause. Use the retained inventory, diagnostic and event evidence to investigate; absence of captured events does not rule out a change.</p><p>{f.inventoryNote}</p><div className="metric-next"><Link className="x" to={fixedLink(f.inventory,d)}>{f.inventoryLabel}</Link><Link className="x" to={fixedLink('/events',d)}>Inspect captured events</Link><button className="btn" onClick={()=>setEvidence(d)}>Findings in this interval</button><Link className="x" to={fixedLink('/data-history',d)}>Inspect retained coverage</Link></div><p>These links use the displayed system-wide interval. Inventory pages describe their own newest-collection scope; they are not historical set-difference proofs. Independent reads are not a universal collection pin.</p></section>
 </>}</QueryView>
 {evidence&&<Dialog open fullScreen title="Findings in aggregate interval" onClose={()=>setEvidence(undefined)}><p>Selected interval: {stamp(evidence.start)} to {stamp(evidence.end)}. Selection stays fixed while observations refresh.</p><FindingIntervalEvidence from={evidence.start} end={evidence.end} states={['firing','changed','flapping','resolved','unknown']}/></Dialog>}
 </div>;
}

import {useEffect,useState,type FormEvent} from 'react';
import {Link,useLocation,useParams,useSearchParams} from 'react-router';
import {useMetricComparison,type MetricComparison,type MetricComparisonRow} from '../api/metricComparison';
import type {TrendSeries,MetricObservations} from '../api/overview';
import type {Unit} from '../api/model';
import {AggregateMetricInvestigation} from './AggregateMetricInvestigation';
import {aggregateMetric,aggregateMetrics} from '../lib/aggregateMetrics';
import {ResourceMetricInvestigation} from './ResourceMetricInvestigation';
import {resourceMetric,resourceMetrics} from '../lib/resourceMetrics';
import {metricFamilies,metricFamily,type MetricFamily} from '../lib/metricNavigation';
import {useTimeRange} from '../lib/useTimeRange';
import {usePageTitle} from '../lib/usePageTitle';
import {globalQuery} from '../lib/globalQuery';
import {sortRows,useSort} from '../lib/sort';
import {fmtValue} from '../lib/units';
import {bucketLabel} from '../lib/timeRange';
import {QueryView,RLink,Empty} from '../components/common';
import {SortTh} from '../components/SortHeader';
import {TrendChart,type Layer} from '../components/TrendChart';
import {TrafficCounterEvidence} from '../components/TrafficCounterEvidence';
import {Dialog} from '../components/Dialog';
import {FindingIntervalEvidence} from '../components/FindingIntervalEvidence';
import {useReportStale} from '../components/StaleContext';

const when=(at:number|undefined)=>at===undefined||!Number.isFinite(at)?'Unknown':new Date(at).toISOString().replace('.000Z','Z');
function evidencePath(path:string,d:MetricComparison,account:string,server=''){
 const [base,own]=path.split('?');const p=new URLSearchParams(globalQuery({kind:'fixed',from:d.start,to:d.end},d.system,account));new URLSearchParams(own).forEach((v,k)=>p.set(k,v));if(server)p.set('server',server);return `${base}?${p}`;
}
function fallbackUnit(f:MetricFamily):Unit{return f.key==='cpu'?'percent':f.key==='memory'?'bytes':f.key==='messages'?'per_second':f.key==='bytes'?'bytes_per_second':'count'}
function format(value:number|undefined,s:TrendSeries|undefined,f:MetricFamily){return value===undefined||!Number.isFinite(value)?'Unknown':fmtValue(value,s?.unit??fallbackUnit(f))}
function Plot({s,label,f,d,error}:{s?:TrendSeries;label:string;f:MetricFamily;d:MetricComparison;error?:string}){
 const band=s?.points.some(p=>p.low!==undefined&&p.high!==undefined)&&s.metric.startsWith('cluster.');const layers:Layer[]=band?[{kind:'band',label:'Observed server bounds',style:{tone:'accent'},points:s!.points.map(p=>({time:p.time,low:p.low??p.value,high:p.high??p.value}))},{kind:'line',label:'Median',style:{tone:'accent'},points:s!.points}]:[{kind:'line',label,style:{tone:'accent'},points:s?.points??[]}];
 if(band&&s?.top)layers.push({kind:'line',label:`${s.top} · highest sample mean`,style:{tone:'info',dash:true},points:s.topPoints});
 const last=s?.points.at(-1)?.value;
 return <div className="metric-fleet-series"><TrendChart intervalActions metricNavigation={false} investigation={{metric:s?.metric,entity:s?.entity}} title={label} now={last===undefined?undefined:`${format(last,s,f)} · last bucket`} fmt={v=>format(v,s,f)} bucketNote={band?'Bucket means of medians; bounds are observed server extremes':f.summary==='count'?'Last count in bucket':'Mean of known metric values in bucket'} summary={s?`${bucketLabel(s.stepMs??0)} buckets; unknown values can occur inside populated buckets`:undefined} start={d.start} end={d.end} stepMs={s?.stepMs} layers={layers} contiguousBuckets error={error}/>{f.summary==='rate'&&<div className="metric-counter-note"><p>Comparable observed increase ({f.key==='bytes'?'bytes':'messages'}):</p><TrafficCounterEvidence observations={s?.observations}/></div>}</div>;
}
export function MetricInvestigation(){
 const key=useParams().family;const f=key?metricFamily(key):undefined;const resource=key?resourceMetric(key):undefined;const aggregate=key?aggregateMetric(key):undefined;usePageTitle(f?`${f.title} investigation`:resource?`${resource.title} investigation`:aggregate?`${aggregate.title} investigation`:'Metric investigations');
 if(aggregate)return <AggregateMetricInvestigation key={aggregate.key} family={aggregate}/>;
 if(resource)return <ResourceMetricInvestigation key={resource.key} family={resource}/>;
 if(!f)return <><h1>{key?'Metric not found':'Metric investigations'}</h1><p>Choose a metric to compare captured server observations.</p><ul>{[...metricFamilies,...resourceMetrics,...aggregateMetrics].map(x=><li key={x.key}><RLink to={`/metrics/${x.key}`}>{x.title}</RLink></li>)}</ul></>;
 return <MetricPage key={f.key} f={f}/>;
}
function MetricPage({f}:{f:MetricFamily}){
 const {spec}=useTimeRange();const [params,setParams]=useSearchParams();const location=useLocation();const server=params.get('server')??'',filter=params.get('q')??'',page=Number(params.get('page')??0),account=params.get('account')??'';const [text,setText]=useState(filter);const [expand,setExpand]=useState<string>();const [evidence,setEvidence]=useState<{d:MetricComparison;server:string}>();
 const q=useMetricComparison(spec,f,server,filter,page);useReportStale(q);const [sort,onSort]=useSort();
 useEffect(()=>{setText(filter)},[filter]);useEffect(()=>{setExpand(undefined);setEvidence(undefined)},[location.pathname,location.search]);
 const choosePage=(n:number)=>setParams(p=>{const next=new URLSearchParams(p);next.set('page',String(n));return next});
 const apply=(e:FormEvent)=>{e.preventDefault();setParams(p=>{const next=new URLSearchParams(p);if(text.trim())next.set('q',text.trim());else next.delete('q');next.delete('page');return next})};
 const all=new URLSearchParams(params);all.delete('server');all.delete('page');
 return <div className="metric-page"><nav className="crumbs" aria-label="Breadcrumb"><RLink to="/servers">Servers</RLink><RLink to="/metrics">Metrics</RLink></nav><h1>{f.title} investigation</h1><p>{f.explanation}</p>
 <nav className="metric-family-links" aria-label="Metric investigations">{metricFamilies.map(x=><RLink key={x.key} to={`/metrics/${x.key}${server?`?server=${encodeURIComponent(server)}`:''}`} aria-current={x.key===f.key?'page':undefined}>{x.title}</RLink>)}<RLink to="/capacity/storage-change">Storage change</RLink><RLink to={`/servers/slow-consumers${server?`?server=${encodeURIComponent(server)}`:''}`}>Slow-consumer detections</RLink></nav>
 <p>Server metrics cover all accounts.{account&&` Account ${account} is retained as context for evidence pages that support it.`} Histories group by reported server name across process lifetimes; simultaneous processes with the same name cannot be separated here.</p>
 {server&&<p>Selected server name: <strong>{server}</strong>. <Link className="x" to={`/metrics/${f.key}?${all}`}>Compare all server names</Link></p>}
 <form className="metric-tools" onSubmit={apply}><label>Filter server names<input value={text} onChange={e=>setText(e.target.value)} maxLength={512}/></label><button className="btn" type="submit">Apply filter</button><button className="btn" type="button" disabled={q.isFetching} onClick={()=>void q.refetch()}>Refresh observations</button></form>
 {f.summary==='rate'&&<p>Comparable counter increases are not a complete traffic volume: only valid report pairs wholly inside this interval contribute. Rates are not integrated; first reports, missing counters, resets, excessive gaps and pairs crossing the start are excluded. Comparison spans do not establish complete capture.</p>}
 <QueryView q={q} what={`${f.title.toLowerCase()} observations`}>{d=>{
 const allowed=['name',...f.metrics.flatMap((_,i)=>[`latest-${i}`,`middle-${i}`,`maximum-${i}`])];const active=sort&&allowed.includes(sort.key)?sort:undefined;
 const rows=active?sortRows(d.rows,r=>{if(active.key==='name')return r.name;const [field,index]=active.key.split('-');const o=r.series[Number(index)]?.observations;return field==='latest'?o?.latest:field==='maximum'?o?.maximum:f.summary==='count'?o?.minimum:o?.mean},active.dir):d.rows;
 return <><p className="metric-window">Requested interval: <time>{when(d.start)}</time> to <time>{when(d.end)}</time>. Source clock: {when(d.readAt)}. {q.isFetching&&'Refreshing; showing the last completed comparison.'}</p>
 <div className="metric-charts">{f.fleetMetrics.map((metric,i)=><Plot key={metric} s={d.fleet[i]} label={`Fleet ${f.labels[i]}`} f={f} d={d} error={d.fleetErrors[i]}/>)}</div>
 <p>Charts show bucket aggregates; table peaks use captured metric values before bucketing. Means are sample-weighted, not time-weighted. Missing values are unknown. Counts below describe returned metric rows, not complete collection coverage.</p>
 <section aria-label="Server comparison"><div className="sec-h"><h2>Server observations</h2><span>{d.rows.length?`${d.page*12+1}–${d.page*12+d.rows.length}`:'0'} of {d.total} server names</span></div><p>Names are paged alphabetically. Sorting compares this loaded page only; it does not rank the whole fleet. Process starts include initial starts, not only restarts.</p>
 {rows.length===0?<Empty>No server names match the returned inventory and filters in this interval.</Empty>:<div className="table-wrap metric-table-region" tabIndex={0} role="region" aria-label="Server observation table"><table className="tbl metric-table" aria-label="Server observations"><thead><tr><SortTh id="name" label="Server name" first="asc" sort={active} onSort={onSort}/>{f.metrics.map((_,i)=><MetricHeaders key={i} f={f} index={i} sort={active} onSort={onSort}/>)}<th scope="col">Known / returned</th><th scope="col">Process starts</th><th scope="col">Evidence</th></tr></thead><tbody>{rows.map(r=><MetricRow key={r.name} r={r} d={d} f={f} account={account} expanded={expand===r.name} onExpand={()=>setExpand(x=>x===r.name?undefined:r.name)} onFindings={()=>setEvidence({d,server:r.name})}/>)}</tbody></table></div>}
 <div className="page-actions"><button className="btn" disabled={d.page===0||q.isFetching} onClick={()=>choosePage(d.page-1)}>Previous servers</button><button className="btn" disabled={d.page+1>=d.pages||q.isFetching} onClick={()=>choosePage(d.page+1)}>Next servers</button></div></section>
 <div className="metric-next"><button className="btn" onClick={()=>setEvidence({d,server})}>Findings in this interval</button><Link className="x" to={evidencePath('/events',d,account,server)}>Events in this interval</Link><Link className="x" to={evidencePath('/data-history',d,'')}>Check retained coverage</Link></div><p>Related findings and events are contextual evidence, not proof of a cause. Live reads can change as collections arrive; this view does not establish a universal collection pin.</p></>;
 }}</QueryView>
 {evidence&&<Dialog open fullScreen title={evidence.server?`Findings for ${evidence.server}`:'Findings in metric interval'} onClose={()=>setEvidence(undefined)}><p>Selected {f.title.toLowerCase()} interval: {when(evidence.d.start)} to {when(evidence.d.end)}. This selection stays fixed while observations refresh.</p><FindingIntervalEvidence key={`${evidence.d.system}|${evidence.d.start}|${evidence.d.end}|${evidence.server}`} from={evidence.d.start} end={evidence.d.end} states={['firing','changed','flapping','resolved','unknown']} entity={evidence.server?{kind:'server',key:evidence.server,server:evidence.server,name:evidence.server,account:'',stream:'',consumer:''}:undefined}/></Dialog>}
 </div>;
}
function MetricHeaders({f,index,sort,onSort}:{f:MetricFamily;index:number;sort:ReturnType<typeof useSort>[0];onSort:ReturnType<typeof useSort>[1]}){const prefix=f.metrics.length>1?`${f.labels[index]} `:'';return <><SortTh id={`latest-${index}`} label={`${prefix}Latest observed`} sort={sort} onSort={onSort}/><SortTh id={`middle-${index}`} label={`${prefix}${f.summary==='count'?'Observed minimum':'Sample mean'}`} sort={sort} onSort={onSort}/><SortTh id={`maximum-${index}`} label={prefix?`${prefix}observed peak`:"Observed peak"} sort={sort} onSort={onSort}/>{f.summary==='rate'&&<th scope="col">{prefix}comparable increase ({f.key==='bytes'?'bytes':'messages'})</th>}</>}
function MetricRow({r,d,f,account,expanded,onExpand,onFindings}:{r:MetricComparisonRow;d:MetricComparison;f:MetricFamily;account:string;expanded:boolean;onExpand:()=>void;onFindings:()=>void}){return <><tr><th scope="row"><Link className="x" to={evidencePath(`/servers/${encodeURIComponent(r.name)}`,d,account)}>{r.name}</Link><button className="btn" onClick={onExpand} aria-expanded={expanded}>{expanded?'Hide history':'Show history'}</button></th>{f.metrics.map((metric,i)=>{const s=r.series[i],o=s?.observations;return <MetricCells key={metric} s={s} o={o} f={f}/>})}<td>{f.metrics.map((metric,i)=>{const o=r.series[i]?.observations;return <p key={metric}>{f.metrics.length>1&&`${f.labels[i]}: `}{o?`${o.knownCount.toLocaleString()} / ${o.sampleCount.toLocaleString()}`:'Summary unavailable'}{r.errors[i]&&` · ${r.errors[i]}`}</p>})}</td><td>{r.metadata?r.metadata.restarts.length.toLocaleString():'Unknown'}</td><td><button className="btn" onClick={onFindings}>Findings for {r.name}</button><Link className="x" to={evidencePath('/events',d,account,r.name)}>Events for {r.name}</Link><Link className="x" to={evidencePath(`/servers/${encodeURIComponent(r.name)}`,d,account)}>Open server evidence</Link></td></tr>{expanded&&<tr><td colSpan={f.metrics.length*(f.summary==='rate'?4:3)+4}><div className="metric-charts metric-expanded">{f.metrics.map((metric,i)=><Plot key={metric} s={r.series[i]} label={`${r.name} ${f.labels[i]}`} f={f} d={d} error={r.errors[i]}/>)}</div><p>Returned observation bounds: {f.metrics.map((_,i)=>`${f.labels[i]} ${when(r.series[i]?.observations?.firstSampleAt)} to ${when(r.series[i]?.observations?.lastSampleAt)}`).join('; ')}. Latest reported process: {r.metadata?.serverId||'Unknown'}. Open server evidence for process lifetimes and diagnostic measurements.</p></td></tr>}</>}
function MetricCells({s,o,f}:{s?:TrendSeries;o?:MetricObservations;f:MetricFamily}){return <><td>{format(o?.latest,s,f)}<small>{when(o?.lastSampleAt)}</small></td><td>{format(f.summary==='count'?o?.minimum:o?.mean,s,f)}</td><td>{format(o?.maximum,s,f)}<small>{when(o?.maximumAt)}</small></td>{f.summary==='rate'&&<td><TrafficCounterEvidence observations={o}/></td>}</>}

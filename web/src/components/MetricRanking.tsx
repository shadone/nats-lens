import {useEffect,useState,type FormEvent} from 'react';
import {useSearchParams,Link} from 'react-router';
import {useMetricRanking,type MetricRankingKind,type GlobalMetricRanking} from '../api/metricRanking';
import type {Entity} from '../api/model';
import type {MetricObservations} from '../api/overview';
import {MetricRanking} from '../gen/natslens/v1/metric_contributors_pb';
import {useTimeRange} from '../lib/useTimeRange';
import {rangeKey} from '../lib/timeRange';
import {metricDestination} from '../lib/metricNavigation';
import {entityPath} from '../lib/view';
import {globalQuery,withGlobal} from '../lib/globalQuery';
import {fmtValue} from '../lib/units';
import {QueryView} from './common';
import {ListTraversal,useListTraversal} from './ListTraversal';
const options={name:MetricRanking.NAME,latest:MetricRanking.LATEST,mean:MetricRanking.MEAN,maximum:MetricRanking.MAXIMUM,minimum:MetricRanking.MINIMUM,total:MetricRanking.OBSERVED_TOTAL,increase:MetricRanking.COUNTER_INCREASE};
const labels={name:'Entity name',latest:'Latest observed',mean:'Sample mean',maximum:'Observed peak',minimum:'Observed minimum',total:'Observed event total',increase:'Comparable counter increase'};
const stamp=(at:number|undefined)=>at===undefined||!Number.isFinite(at)?'Unknown':new Date(at).toISOString().replace('.000Z','Z');
function Stamp({at}:{at:number|undefined}){return at===undefined||!Number.isFinite(at)?<span>Unknown</span>:<time dateTime={new Date(at).toISOString()}>{stamp(at)}</time>}
function value(o:MetricObservations|undefined,by:keyof typeof options,d:GlobalMetricRanking){if(by==='name')return '—';if(by==='total'||by==='increase'){const n=by==='total'?o?.observedTotal:o?.counterComparisons?.observedIncrease;return n!==undefined&&/^(0|[1-9]\d*)$/.test(n)?BigInt(n).toLocaleString():'Unknown'}const n=o?.[by];return n===undefined||!Number.isFinite(n)?'Unknown':fmtValue(n,d.unit)}
function path(e:Entity,metric:string,d:GlobalMetricRanking,account:string){return withGlobal(metricDestination({metric,entity:e})??entityPath(e)!,globalQuery({kind:'fixed',from:d.start,to:d.end},d.system,e.account||account))}
export function MetricRankingPanel({metrics,kind,account='',stream='',query=''}:{metrics:string[];kind:MetricRankingKind;account?:string;stream?:string;query?:string}){
 const {spec}=useTimeRange();const [params,setParams]=useSearchParams();const open=params.get('rank')==='1';const metric=metrics.includes(params.get('rankMetric')??'')?params.get('rankMetric')!:metrics[0];
 const counter=/^server\.(in|out)_(msgs|bytes)_rate$/.test(metric),summed=metric==='server.slow_consumers';
 const available=(Object.keys(options) as (keyof typeof options)[]).filter(x=>x!=='increase'||counter).filter(x=>x!=='total'||summed);
 const raw=params.get('rankBy') as keyof typeof options;const by=available.includes(raw)?raw:'maximum',descending=params.get('rankOrder')!=='asc';
 const nameFilter=kind==='server'?query:params.get('rankQuery')??'';const [filterInput,setFilter]=useState(nameFilter);useEffect(()=>setFilter(nameFilter),[nameFilter]);
 const scope={account:kind==='server'?undefined:account,stream:kind==='server'?undefined:stream,query:nameFilter};
 const traversal=useListTraversal(JSON.stringify([params.get('system'),rangeKey(spec),metric,by,descending,scope]));
 const q=useMetricRanking(spec,metric,kind,options[by],descending,scope,traversal.token,open);
 const filter=(e:FormEvent)=>{e.preventDefault();set('rankQuery',filterInput.trim())};
 const set=(key:string,v:string)=>setParams(p=>{const n=new URLSearchParams(p);if(v)n.set(key,v);else n.delete(key);return n});
 return <section className="metric-ranking" aria-label="Global metric ranking"><button className="btn" aria-expanded={open} onClick={()=>set('rank',open?'':'1')}>{open?'Hide global ranking':`Rank matching ${kind==='server'?'server names':'resources'}`}</button>{open&&<>
 <h2>Global metric ranking</h2><p>Ordering uses all matching entities observed in this interval, before paging. Ordering and summaries are frozen together for continuation; independent histories and evidence are not pinned. Missing values sort last in either direction. This does not prove complete capture or causal contribution.</p>
 <div className="metric-tools"><label>Ranking metric<select value={metric} onChange={e=>set('rankMetric',e.target.value)}>{metrics.map(m=><option key={m} value={m}>{m}</option>)}</select></label><label>Rank by<select value={by} onChange={e=>set('rankBy',e.target.value)}>{available.map(k=><option key={k} value={k}>{labels[k]}</option>)}</select></label><label>Ranking order<select value={descending?'desc':'asc'} onChange={e=>set('rankOrder',e.target.value)}><option value="desc">Highest first</option><option value="asc">Lowest first</option></select></label></div>
 {kind!=='server'&&<form className="metric-tools" onSubmit={filter}><label>Filter ranking names<input value={filterInput} maxLength={512} onChange={e=>setFilter(e.target.value)}/></label><button className="btn">Apply ranking filter</button></form>}
 <QueryView q={q} what="global metric ranking">{d=><><p>Ranking interval: {stamp(d.start)} to {stamp(d.end)}. Source clock at capture: {stamp(d.readAt)}. {kind==='server'?'All accounts.':account?`Account ${account}${stream?` / stream ${stream}`:''}.`:'All accounts.'} {nameFilter&&`Name filter: ${nameFilter}.`} {spec.kind==='live'&&(traversal.token?'This continuation is fixed; Refresh list starts a new ranking.':'The first page refreshes automatically.')}</p>
 {!d.rows.length?<p>No matching observed entities. Missing capture is not measured zero.</p>:<div className="table-wrap metric-table-region" role="region" aria-label="Global ranking table" tabIndex={0}><table className="tbl metric-table" aria-label="Global metric ranking"><thead><tr><th scope="col">Entity</th><th scope="col">{labels[by]}</th><th scope="col">Known / returned</th><th scope="col">Evidence</th></tr></thead><tbody>{d.rows.map(r=><tr key={r.entity.key}><th scope="row">{r.entity.key}</th><td>{value(r.observations,by,d)}{(by==='latest'||by==='maximum')&&<small><Stamp at={by==='latest'?r.observations?.lastSampleAt:r.observations?.maximumAt}/></small>}</td><td>{r.observations?`${r.observations.knownCount.toLocaleString()} / ${r.observations.sampleCount.toLocaleString()}`:'Unknown'}<small><Stamp at={r.observations?.firstSampleAt}/> to <Stamp at={r.observations?.lastSampleAt}/></small>{by==='increase'&&<small>{r.observations?.counterComparisons?.comparisonCount.toLocaleString()??'Unknown'} comparable report pairs; not complete traffic volume</small>}</td><td><Link className="x" to={path(r.entity,metric,d,account)}>Investigate {r.entity.name||r.entity.key}</Link><Link className="x" to={withGlobal(entityPath(r.entity)!,globalQuery({kind:'fixed',from:d.start,to:d.end},d.system,r.entity.account||account))}>Open diagnostics</Link></td></tr>)}</tbody></table></div>}
 <ListTraversal traversal={traversal} next={d.next} count={d.total} loaded={d.rows.length} error={q.error} refreshing={q.isFetching} refresh={()=>void q.refetch()}/></>}</QueryView>
 {q.error&&!q.data&&<ListTraversal traversal={traversal} loaded={0} error={q.error} refreshing={q.isFetching} refresh={()=>void q.refetch()}/>}
 <p>Capture is limited to 20,000 observed entities and 2 MiB of summaries. Narrow filters when the source reports a limit; overflow is an error, not a partial global rank. Shared URLs preserve ranking choices and bounds; the page cursor is local to this session.</p>
 </>}</section>;
}

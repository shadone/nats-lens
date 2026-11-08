import type {InvestigationSelection} from './investigationContext';
export type MetricFamilyKey='cpu'|'memory'|'connections'|'messages'|'bytes';
export interface MetricFamily {key:MetricFamilyKey;title:string;metrics:string[];fleetMetrics:string[];labels:string[];explanation:string;summary:'gauge'|'count'|'rate'}
export const metricFamilies:MetricFamily[]=[
 {key:'cpu',title:'CPU',metrics:['server.cpu_percent'],fleetMetrics:['cluster.cpu_percent'],labels:['CPU'],summary:'gauge',explanation:'NATS process CPU as percent of one core; values may exceed 100%. This is not host saturation. A fleet band shows per-collection medians and observed server bounds.'},
 {key:'memory',title:'Memory',metrics:['server.mem_bytes'],fleetMetrics:['cluster.mem_bytes'],labels:['Resident memory'],summary:'gauge',explanation:'Resident memory reported by NATS processes, in bytes. This is not host memory pressure. The fleet band shows medians and observed server bounds.'},
 {key:'connections',title:'Connections',metrics:['server.connections'],fleetMetrics:['cluster.connections'],labels:['Connections'],summary:'count',explanation:'Reported client connection counts. A count is not a list of identified clients and does not attribute a change to a particular application.'},
 {key:'messages',title:'Message traffic',metrics:['server.in_msgs_rate','server.out_msgs_rate'],fleetMetrics:['cluster.in_msgs_rate','cluster.out_msgs_rate'],labels:['In','Out'],summary:'rate',explanation:'Messages received and sent per second. Rates compare cumulative counters within a process; resets and long gaps have no comparison. Directions are separate, not a net rate.'},
 {key:'bytes',title:'Byte traffic',metrics:['server.in_bytes_rate','server.out_bytes_rate'],fleetMetrics:['cluster.in_bytes_rate','cluster.out_bytes_rate'],labels:['In','Out'],summary:'rate',explanation:'Bytes received and sent per second, separately. These are traffic rates, not retained storage or an exact volume over the range.'},
];
export function metricFamily(key:string):MetricFamily|undefined{return metricFamilies.find(f=>f.key===key)}
export function metricDestination(selection:InvestigationSelection|undefined):string|undefined{
 const sources=selection?.series?.length?selection.series.map(s=>({metric:s.metric,entity:s.entity})):[selection];
 const known=sources.filter((s):s is NonNullable<typeof s>=>!!s?.metric);
 if(!known.length)return;
 const paths=known.map(s=>{const family=metricFamilies.find(f=>[...f.metrics,...f.fleetMetrics].includes(s.metric!));if(family)return `/metrics/${family.key}`;if(s.metric==='server.slow_consumers'||s.metric==='cluster.slow_consumers')return '/servers/slow-consumers';if(['cluster.js_storage_bytes','server.js_storage_bytes','cluster.capacity_file_store_bytes','server.capacity_file_store_bytes'].includes(s.metric!))return '/capacity/storage-change';return undefined});
 if(paths.some(p=>!p)||new Set(paths).size!==1)return;
 const path=paths[0]!;const names=[...new Set(known.map(s=>s.entity?.kind==='server'?(s.entity.server||s.entity.key):''))];const p=new URLSearchParams();
 if(names.length===1&&names[0])p.set(path==='/capacity/storage-change'?'sourceServer':'server',names[0]);
 return path+(p.size?`?${p}`:'');
}

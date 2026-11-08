import type {Entity,Unit} from '../api/model';
export interface ResourceMetric {key:string;metric:string;kind:'stream'|'consumer';title:string;unit:Unit;summary:'count'|'gauge'|'rate';explanation:string}
export const resourceMetrics:ResourceMetric[]=[
 {key:'stream-messages',metric:'stream.messages',kind:'stream',title:'Stored messages',unit:'count',summary:'count',explanation:'Messages retained in each stream, from the leader view (the largest report when no server leads). A stored count is not ingress, delivery throughput or a count of KV keys or objects.'},
 {key:'stream-bytes',metric:'stream.bytes',kind:'stream',title:'Logical storage bytes',unit:'bytes',summary:'gauge',explanation:'Logical bytes retained in each stream, from the leader view. This excludes replica multiplication and is not physical file-store usage. KV and object stores use backing-stream bytes, not per-key or per-object measurements.'},
 {key:'stream-replica-bytes',metric:'stream.observed_replica_bytes',kind:'stream',title:'Observed replica bytes',unit:'bytes',summary:'gauge',explanation:'Sum of logical bytes in reported replica copies. Missing replicas are not estimated. This is neither configured-replica storage nor physical filesystem usage.'},
 {key:'stream-ingest',metric:'stream.msgs_rate',kind:'stream',title:'Stream ingestion rate',unit:'per_second',summary:'rate',explanation:'Messages stored per second inferred from comparable last-sequence increases, using the leader view or highest report. A recreated stream has no initial rate. This is not a count of retained messages or an exact range volume.'},
 {key:'consumer-pending',metric:'consumer.num_pending',kind:'consumer',title:'Consumer pending',unit:'count',summary:'count',explanation:'Messages not yet delivered, from the consumer leader view. Missing leadership is unknown; follower zeros do not establish an empty backlog. This is distinct from server slow-consumer detections.'},
 {key:'consumer-ack-pending',metric:'consumer.num_ack_pending',kind:'consumer',title:'Consumer ack pending',unit:'count',summary:'count',explanation:'Messages delivered and not yet acknowledged, from the leader view or largest report without a leader. A pending count alone does not identify the application or cause.'},
 {key:'consumer-redelivered',metric:'consumer.num_redelivered',kind:'consumer',title:'Consumer redelivered',unit:'count',summary:'count',explanation:'Messages reported as delivered more than once, from the leader view or largest report. This is a reported state count, not new redelivery events during the interval and not unique affected clients.'},
 {key:'consumer-delivery',metric:'consumer.delivered_rate',kind:'consumer',title:'Consumer delivery rate',unit:'per_second',summary:'rate',explanation:'Messages delivered per second from comparable leader consumer-sequence increases. Recreation and long collection gaps break comparison. A delivery rate is not an acknowledgement rate or a server slow-consumer counter.'},
];
export function resourceMetric(key:string):ResourceMetric|undefined{return resourceMetrics.find(f=>f.key===key)}
export function resourceMetricDestination(metric:string,entity:Entity|undefined):string|undefined{
 const f=resourceMetrics.find(x=>x.metric===metric);if(!f||!entity||entity.kind!==f.kind)return;
 const p=new URLSearchParams({[f.kind]:entity.key});return `/metrics/${f.key}?${p}`;
}
export function resourceSelection(family:ResourceMetric,params:URLSearchParams):Entity|undefined{
 const key=params.get(family.kind);if(key===null)return;const parts=key.split('/');
 // ASVS 1.2.2: use only allowlisted local metric routes and typed, encoded identities.
 if(parts.length!==(family.kind==='stream'?2:3)||parts.some(x=>!x)||key.length>1024)throw new Error('Invalid resource selection; use the account/stream or account/stream/consumer identity.');
 const [account,stream,consumer='']=parts;return {kind:family.kind,key,name:consumer||stream,account,stream,consumer,server:''};
}

import {fromJson} from '@bufbuild/protobuf';
import {TimestampSchema} from '@bufbuild/protobuf/wkt';
import type {Entity,EntityKind,FrozenEvidenceRange} from '../api/model';
import {epoch} from '../api/findingEpisodes';
import {writeRange} from './timeRange';
const kinds:EntityKind[]=['system','server','account','stream','consumer','connection','client'];
export function historyEntity(p:URLSearchParams):Entity|undefined{
 const type=p.get('type'),key=p.get('key')??'';if(type===null&&!p.has('key'))return undefined;
 if(!kinds.includes(type as EntityKind)||key.length>256||type==='system'&&key!==''||type!=='system'&&!key)throw new Error('Invalid retained-history resource');
 const e:Entity={kind:type as EntityKind,key,name:key,account:'',stream:'',consumer:'',server:''},parts=key.split('/');
 const required=type==='stream'?2:type==='consumer'?3:type==='connection'?2:type==='client'||type==='system'?0:1;
 if(required&&(parts.length!==required||parts.some(x=>!x)))throw new Error('Invalid retained-history resource');
 if(type==='connection'&&!/^(0|[1-9]\d*)$/.test(parts[1]))throw new Error('Invalid connection identity');
 if(type==='account')e.account=key;if(type==='server'||type==='connection')e.server=parts[0];if(type==='stream'||type==='consumer'){e.account=parts[0];e.stream=parts[1];if(type==='consumer')e.consumer=parts[2]}
 return e;
}
export function findingHistoryRange(p:URLSearchParams){
 const range={start:p.get('from')??'',end:p.get('to')??''};const a=fromJson(TimestampSchema,range.start),b=fromJson(TimestampSchema,range.end);
 if(a.seconds>b.seconds||a.seconds===b.seconds&&a.nanos>=b.nanos)throw new Error('Invalid history interval');
 const readEpoch=p.get('readEpoch')??undefined;if(readEpoch!==undefined)epoch(readEpoch);
 return {range,from:Date.parse(range.start),end:Date.parse(range.end),readEpoch,checkCode:p.get('check')||undefined,entity:historyEntity(p)};
}
export function findingHistoryPath(from:number,end:number,readEpoch:string|undefined,system:string,checkCode?:string,entity?:Entity,range?:FrozenEvidenceRange){
 const p=writeRange(new URLSearchParams({system}),{kind:'fixed',from,to:end});if(range){p.set('from',range.start);p.set('to',range.end)}
 if(readEpoch!==undefined)p.set('readEpoch',readEpoch);if(checkCode)p.set('check',checkCode);if(entity){p.set('type',entity.kind);if(entity.key)p.set('key',entity.key)}return `/findings/history?${p}`;
}
export function findingHistoryRequest(h:ReturnType<typeof findingHistoryRange>){
 return {range:h.range,readEpoch:h.readEpoch,checkCode:h.checkCode,pageSize:50,...(h.entity?{entity:{type:`ENTITY_TYPE_${h.entity.kind.toUpperCase()}`,key:h.entity.key,account:h.entity.account,stream:h.entity.stream,consumer:h.entity.consumer,server:h.entity.server}}:{})};
}

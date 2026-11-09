import {fromJson} from '@bufbuild/protobuf';
import {TimestampSchema} from '@bufbuild/protobuf/wkt';
import type {FrozenEvidenceRange} from './model';

export function eventReadId(raw:string|undefined):bigint|undefined{
 if(raw===undefined)return undefined;
 if(!/^(0|[1-9]\d{0,18})$/.test(raw)||BigInt(raw)>9223372036854775807n)throw new Error('The event membership boundary is invalid.');
 return BigInt(raw);
}
export function frozenEventRange(range:FrozenEvidenceRange){
 const start=fromJson(TimestampSchema,range.start),end=fromJson(TimestampSchema,range.end);
 if(start.seconds>end.seconds||start.seconds===end.seconds&&start.nanos>=end.nanos)throw new Error('The selected evidence interval is invalid.');
 return {start,end};
}
export function verifyEventSelection(reply:{selectedIds:string[];eventReadId?:bigint;events:{id:string}[];totalCount:number},ids:string[],boundary?:string){
 if(reply.selectedIds.length!==ids.length||reply.selectedIds.some((v,i)=>v!==ids[i]))throw new Error('This server did not acknowledge the exact event selection.');
 if(boundary!==undefined&&reply.eventReadId!==eventReadId(boundary))throw new Error('This server did not acknowledge the event membership boundary.');
 const allowed=new Set(ids),seen=new Set<string>();
 if(reply.totalCount<0||reply.totalCount>ids.length)throw new Error('The returned event count exceeds the selected identities.');
 for(const e of reply.events){if(!allowed.has(e.id)||seen.has(e.id))throw new Error('The server returned an event outside the exact selection.');seen.add(e.id)}
}

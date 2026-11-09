import {create} from '@bufbuild/protobuf';
import {timestampFromMs} from '@bufbuild/protobuf/wkt';
import {expect,it} from 'vitest';
import {timeline} from './adapt-findings';
import {GetStatusTimelineResponseSchema} from '../gen/natslens/v1/timeline_pb';
import {EntityType} from '../gen/natslens/v1/common_pb';
it('retains exact evidence IDs, affected identities and resolved query bounds',()=>{
 const at=timestampFromMs(Date.parse('2026-10-17T23:59:00Z'));at.nanos=123456000;
 const t=timeline(create(GetStatusTimelineResponseSchema,{eventReadId:9007199254740995n,range:{start:at,end:{...at,seconds:at.seconds+60n}},nats:[{id:'7',eventId:'7',findingId:'stream-001:APP/orders',transitionEpoch:9007199254740993n,entity:{type:EntityType.STREAM,key:'APP/orders'}}],windows:[{id:'window',supportingEventIds:['9007199254740993'],evidenceTotal:1,eventReadId:9007199254740995n,captureRange:{start:at,end:{...at,seconds:at.seconds+60n}},derivation:'Observed grouping'}]}));
 expect(t.eventReadId).toBe('9007199254740995');expect(t.evidenceRange?.start).toBe('2026-10-17T23:59:00.123456Z');
 expect(t.nats[0]).toMatchObject({findingId:'stream-001:APP/orders',transitionEpoch:'9007199254740993',entity:{key:'APP/orders'}});
 expect(t.windows[0]).toMatchObject({id:'window',supportingEventIds:['9007199254740993'],eventReadId:'9007199254740995',captureRange:t.evidenceRange});
});

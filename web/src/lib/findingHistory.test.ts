import {expect,it} from 'vitest';
import {findingHistoryPath,findingHistoryRange,historyEntity} from './findingHistory';
const from='2026-10-17T12:00:00.000123Z',to='2026-10-17T13:00:00.000456Z';
it('keeps canonical typed scope, exact bounds and zero membership in shared links',()=>{
 const e=historyEntity(new URLSearchParams({type:'consumer',key:'APP/orders/worker'}))!;const path=findingHistoryPath(Date.parse(from),Date.parse(to),'0','shop','consumer-001',e,{start:from,end:to});const u=new URL(path,'https://example.com');const selected=findingHistoryRange(u.searchParams);expect(selected.range).toEqual({start:from,end:to});expect(selected.readEpoch).toBe('0');expect(selected.entity).toMatchObject({kind:'consumer',account:'APP',stream:'orders',consumer:'worker'});
});
it('validates all supported resource kinds and rejects ambiguous or invalid keys before a read',()=>{
 for(const [type,key] of [['system',''],['server','nats-0'],['account','APP'],['stream','APP/orders'],['consumer','APP/orders/worker'],['connection','nats-0/42'],['client','checkout-v2']])expect(historyEntity(new URLSearchParams({type,key}))?.kind).toBe(type);
 for(const [type,key] of [['unknown','x'],['stream','orders'],['consumer','APP/orders'],['connection','nats-0/no'],['account',''],['system','not-empty'],['server','x'.repeat(257)]])expect(()=>historyEntity(new URLSearchParams({type,key}))).toThrow();
 for(const p of [new URLSearchParams(),new URLSearchParams({from,to:from}),new URLSearchParams({from,to,readEpoch:'-1'}),new URLSearchParams({from,to,readEpoch:'9223372036854775808'})])expect(()=>findingHistoryRange(p)).toThrow();
});

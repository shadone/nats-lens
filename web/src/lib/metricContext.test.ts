import {expect,it} from 'vitest';
import {buildInvestigationContext} from './investigationContext';
it('copies the selected metric family and server identity from a metric detail route',()=>{
 const c=buildInvestigationContext({origin:'http://example.com',pathname:'/metrics/messages',search:'?system=example-prod&server=nats-0&account=APP',system:'example-prod',spec:{kind:'fixed',from:1000,to:2000}});expect(c.entity?.server).toBe('nats-0');expect(c.series?.map(s=>s.metric)).toEqual(['server.in_msgs_rate','server.out_msgs_rate']);expect(c.range).toEqual({start:new Date(1000).toISOString(),end:new Date(2000).toISOString()});
});
it('retains URL-backed comparison and ranking choices as context rather than dropping them',()=>{
 const c=buildInvestigationContext({origin:'http://example.com',pathname:'/metrics/consumer-pending',search:'?system=shop&account=OTHER&consumer=APP%2Forders%2Fworker&page=2&rank=1&rankMetric=consumer.num_pending&rankBy=maximum&rankOrder=asc&rankQuery=worker',system:'shop',spec:{kind:'fixed',from:1000,to:2000}});
 expect(c.account).toBe('APP');expect(c.entity?.key).toBe('APP/orders/worker');
 expect(c.filters).toMatchObject({page:'2',rank:'1',rankMetric:'consumer.num_pending',rankBy:'maximum',rankOrder:'asc',rankQuery:'worker'});
});
it('uses the consumer comparison stream account before a retained account selector',()=>{
 const c=buildInvestigationContext({origin:'http://example.com',pathname:'/metrics/consumer-pending',search:'?system=shop&account=OTHER&stream=APP%2Forders',system:'shop',spec:{kind:'fixed',from:1000,to:2000}});
 expect(c.account).toBe('APP');expect(c.entity).toMatchObject({type:'ENTITY_TYPE_STREAM',key:'APP/orders',account:'APP',stream:'orders'});
 expect(c.series?.[0].entity).toBeUndefined();
});

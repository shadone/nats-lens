import {expect,it} from 'vitest';
import {buildInvestigationContext} from './investigationContext';
it('copies the selected metric family and server identity from a metric detail route',()=>{
 const c=buildInvestigationContext({origin:'http://example.com',pathname:'/metrics/messages',search:'?system=example-prod&server=nats-0&account=APP',system:'example-prod',spec:{kind:'fixed',from:1000,to:2000}});expect(c.entity?.server).toBe('nats-0');expect(c.series?.map(s=>s.metric)).toEqual(['server.in_msgs_rate','server.out_msgs_rate']);expect(c.range).toEqual({start:new Date(1000).toISOString(),end:new Date(2000).toISOString()});
});

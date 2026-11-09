import {expect,it,vi} from 'vitest';
import {getTimeWindows} from './requests';
const wire=vi.hoisted(()=>({fetch:vi.fn<typeof fetch>()}));
vi.mock('./client',async()=>{
 const {createClient}=await import('@connectrpc/connect');
 const {createConnectTransport}=await import('@connectrpc/connect-web');
 const {LensService}=await import('../gen/natslens/v1/service_pb');
 return {lens:createClient(LensService,createConnectTransport({baseUrl:'http://fixture.invalid/api',fetch:wire.fetch}))};
});
it('serializes only system/range and preserves precise window evidence',async()=>{
 wire.fetch.mockResolvedValue(new Response(JSON.stringify({windows:[{id:'window-1',start:'2026-10-17T14:00:00Z',end:'2026-10-17T14:04:00Z',label:'Node drain',detail:'observed context',supportingEventIds:['9007199254740993'],derivation:'node-drain/v1',unavailable:['window_input_limit'],evidenceTotal:1,eventReadId:'9007199254740995',captureRange:{start:'2026-10-17T13:00:00Z',end:'2026-10-17T15:00:00Z'}}]}),{headers:{'Content-Type':'application/json'}}));
 const controller=new AbortController();
 const result=await getTimeWindows('shop',{kind:'fixed',from:Date.parse('2026-10-17T13:00:00Z'),to:Date.parse('2026-10-17T15:00:00Z')},controller.signal);
 expect(wire.fetch).toHaveBeenCalledTimes(1);
 const [input,init]=wire.fetch.mock.calls[0];
 const request=new Request(input,init);
 expect(request.url).toBe('http://fixture.invalid/api/natslens.v1.LensService/GetTimeWindows');
 expect(await request.json()).toEqual({system:'shop',range:{start:'2026-10-17T13:00:00Z',end:'2026-10-17T15:00:00Z'}});
 expect(result).toEqual([{id:'window-1',start:Date.parse('2026-10-17T14:00:00Z'),end:Date.parse('2026-10-17T14:04:00Z'),label:'Node drain',detail:'observed context',supportingEventIds:['9007199254740993'],derivation:'node-drain/v1',unavailable:['window_input_limit'],evidenceTotal:1,eventReadId:'9007199254740995',captureRange:{start:'2026-10-17T13:00:00Z',end:'2026-10-17T15:00:00Z'}}]);
});

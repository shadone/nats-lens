import {render} from '@testing-library/react';
import {MemoryRouter,Route,Routes} from 'react-router';
import {beforeEach,expect,it,vi} from 'vitest';
import {StreamDetail} from './StreamDetail';
import {ConsumerDetail} from './ConsumerDetail';
const hooks=vi.hoisted(()=>({overview:vi.fn(()=>({data:{windows:[]}})),windows:vi.fn(()=>({data:[{start:1,end:2,label:'Node drain'}]})),chart:vi.fn((_props:{windows?:{label:string}[]})=>null)}));
vi.mock('../api/queries',()=>({
 useOverview:hooks.overview,useTimeWindows:hooks.windows,useSeries:()=>({isPending:false}),
 useStream:()=>({isPending:false,data:{stream:{entity:{kind:'stream',key:'APP/orders',account:'APP',stream:'orders',name:'orders'},status:'ok'},config:[],servers:[],consumers:[],disagreements:[]}}),
 useConsumer:()=>({isPending:false,data:{consumer:{entity:{kind:'consumer',key:'APP/orders/worker',account:'APP',stream:'orders',consumer:'worker',name:'worker'},present:true,durable:true,status:'ok'},config:[],servers:[],disagreements:[]}}),
}));
vi.mock('../lib/useTimeRange',()=>({useTimeRange:()=>({spec:{kind:'fixed',from:1,to:2}}),useResolvedRange:()=>({start:1,end:2})}));
vi.mock('../lib/useGlobalQuery',()=>({useGlobalQuery:()=>''}));
vi.mock('../charts/SeriesChart',()=>({QuerySeriesChart:hooks.chart,SeriesChart:()=>null}));
vi.mock('../components/AccountName',()=>({AccountName:()=>null}));
vi.mock('../components/ConsumersTable',()=>({ConsumersTable:()=>null}));
vi.mock('../components/EntityParts',()=>({Stat:()=>null,Disagreements:()=>null,EntityFindings:()=>null,EntityEvents:()=>null}));
beforeEach(()=>vi.clearAllMocks());
it.each([
 ['/streams/APP/orders','/streams/:account/:stream',false,1],
 ['/kv-stores/APP/orders','/kv-stores/:account/:bucket',false,1],
 ['/object-stores/APP/orders','/object-stores/:account/:bucket',false,1],
 ['/consumers/APP/orders/worker','/consumers/:account/:stream/:consumer',true,1],
] as const)('uses focused annotations instead of Overview for %s',(path,route,consumer,count)=>{
 render(<MemoryRouter initialEntries={[path]}><Routes><Route path={route} element={consumer?<ConsumerDetail/>:<StreamDetail/>}/></Routes></MemoryRouter>);
 expect(hooks.overview).not.toHaveBeenCalled();expect(hooks.windows).toHaveBeenCalledTimes(count);
 expect(hooks.chart.mock.calls.filter(([props])=>props.windows?.[0]?.label==='Node drain')).toHaveLength(consumer?3:2);
});

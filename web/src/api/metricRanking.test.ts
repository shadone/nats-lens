import {create} from '@bufbuild/protobuf';
import {timestampFromMs} from '@bufbuild/protobuf/wkt';
import {beforeEach,expect,it,vi} from 'vitest';
import {lens} from './client';
import {getMetricRanking} from './metricRanking';
import {GetLensInfoResponseSchema} from '../gen/natslens/v1/info_pb';
import {ListMetricContributorsResponseSchema,MetricRanking} from '../gen/natslens/v1/metric_contributors_pb';
import {EntityType} from '../gen/natslens/v1/common_pb';
vi.mock('./client',()=>({lens:{getLensInfo:vi.fn(),listMetricContributors:vi.fn()}}));
const now=Date.parse('2026-10-17T14:30:00Z'),range={start:timestampFromMs(now-3600000),end:timestampFromMs(now)};
beforeEach(()=>{vi.mocked(lens.getLensInfo).mockResolvedValue(create(GetLensInfoResponseSchema,{now:timestampFromMs(now)}));vi.mocked(lens.listMetricContributors).mockImplementation(async r=>create(ListMetricContributorsResponseSchema,{metric:r.metric,ranking:r.ranking,descending:r.descending,range,readAt:timestampFromMs(now),unit:3,totalCount:200,nextPageToken:'next',contributors:[{entity:{type:EntityType.SERVER,key:'nats-99',name:'nats-99',server:'nats-99'},observations:{maximum:999}}]}))});
it('ranks the whole native capture and accepts its frozen summaries without inventory/history enumeration',async()=>{
 const d=await getMetricRanking('shop',{kind:'live',preset:'1h'},'server.cpu_percent','server',MetricRanking.MAXIMUM,true,{query:'nats'},'') as unknown as {rows:{entity:{key:string};observations:{maximum:number}}[];total:number;next:string;start:number;end:number};expect(d.rows[0].entity.key).toBe('nats-99');expect(d.rows[0].observations.maximum).toBe(999);expect(d.total).toBe(200);expect(d.next).toBe('next');expect(d.start).toBe(now-3600000);expect(vi.mocked(lens.listMetricContributors).mock.calls[0][0]).toMatchObject({metric:'server.cpu_percent',ranking:MetricRanking.MAXIMUM,descending:true,query:'nats',limit:12,range});
});
it('continues with token bounds rather than resolving a new live interval',async()=>{
 await getMetricRanking('shop',{kind:'live',preset:'1h'},'server.cpu_percent','server',MetricRanking.MAXIMUM,true,{},'next');expect(lens.getLensInfo).not.toHaveBeenCalled();expect(vi.mocked(lens.listMetricContributors).mock.calls[0][0]).toMatchObject({pageToken:'next',range:undefined});
});
it('rejects wrong response identities, metrics and unbounded pages',async()=>{
 for(const response of [{metric:'server.mem_bytes',contributors:[]},{metric:'server.cpu_percent',contributors:[{entity:{type:EntityType.STREAM,key:'APP/orders'}}]},{metric:'server.cpu_percent',contributors:Array.from({length:13},(_,i)=>({entity:{type:EntityType.SERVER,key:`nats-${i}`,server:`nats-${i}`}}))}]){vi.mocked(lens.listMetricContributors).mockResolvedValue(create(ListMetricContributorsResponseSchema,{...response,range}));await expect(getMetricRanking('shop',{kind:'fixed',from:now-3600000,to:now},'server.cpu_percent','server',MetricRanking.MAXIMUM,true,{},'')).rejects.toThrow(/identity|metric|bound/i)}
});

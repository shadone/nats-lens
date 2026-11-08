import {create} from '@bufbuild/protobuf';
import {timestampFromMs} from '@bufbuild/protobuf/wkt';
import {beforeEach,expect,it,vi} from 'vitest';
import {GetLensInfoResponseSchema} from '../gen/natslens/v1/info_pb';
import {ListServersResponseSchema} from '../gen/natslens/v1/servers_pb';
import {GetTimeSeriesBatchResponseSchema,SeriesSchema,ObservationSummarySchema} from '../gen/natslens/v1/timeseries_pb';
import {lens} from './client';
import {getMetricComparison} from './metricComparison';
import type {MetricFamily} from '../lib/metricNavigation';
vi.mock('./client',()=>({lens:{getLensInfo:vi.fn(),listServers:vi.fn(),getTimeSeriesBatch:vi.fn()}}));
const now=Date.parse('2026-10-17T14:30:00Z');const family:MetricFamily={key:'messages',title:'Messages',metrics:['server.in_msgs_rate','server.out_msgs_rate'],fleetMetrics:['cluster.in_msgs_rate','cluster.out_msgs_rate'],labels:['In','Out'],explanation:'Rates',summary:'rate'};
beforeEach(()=>{
 vi.mocked(lens.getLensInfo).mockResolvedValue(create(GetLensInfoResponseSchema,{now:timestampFromMs(now)}));vi.mocked(lens.listServers).mockResolvedValue(create(ListServersResponseSchema,{servers:Array.from({length:25},(_,i)=>({name:`nats-${String(i).padStart(2,'0')}`,serverId:`S${i}`}))}));
 vi.mocked(lens.getTimeSeriesBatch).mockImplementation(async req=>create(GetTimeSeriesBatchResponseSchema,{results:req.queries!.map(q=>({series:create(SeriesSchema,{metric:q.metric,entity:q.entity,observations:create(ObservationSummarySchema,{sampleCount:4n,knownCount:3n,maximum:100,latest:undefined})})}))}));
});
it('freezes one shared live range, enriches only twelve visible servers and keeps native statistics',async()=>{
 const result=await getMetricComparison('example-prod',{kind:'live',preset:'1h'},family,'','',0) as {rows:{name:string;series:{observations?:{maximum?:number;latest?:number}}[]}[];total:number;start:number;end:number};expect(result.rows).toHaveLength(12);expect(result.total).toBe(25);expect(result.start).toBe(now-3600000);expect(result.end).toBe(now);const req=vi.mocked(lens.getTimeSeriesBatch).mock.calls[0][0];expect(req.queries).toHaveLength(26);expect(req.queries?.every(q=>q.includeObservations)).toBe(true);expect(req.queries?.some(q=>q.entity?.key==='nats-12')).toBe(false);expect(result.rows[0].series[0].observations?.maximum).toBe(100);expect(result.rows[0].series[0].observations?.latest).toBeUndefined();expect(req.range).toEqual(vi.mocked(lens.listServers).mock.calls[0][0].range);
});
it('retains a selected historical server, clamps pages and does not mix another server history',async()=>{
 const result=await getMetricComparison('example-prod',{kind:'fixed',from:now-3600000,to:now},family,'nats-24','',7) as {rows:{name:string}[];page:number};expect(result.page).toBe(0);expect(result.rows.map(r=>r.name)).toEqual(['nats-24']);expect(vi.mocked(lens.getTimeSeriesBatch).mock.calls[0][0].queries?.map(q=>q.entity?.key??'')).toEqual(['','','nats-24','nats-24']);
});

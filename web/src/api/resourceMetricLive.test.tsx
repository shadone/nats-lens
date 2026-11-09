import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
import {act,renderHook} from '@testing-library/react';
import {create} from '@bufbuild/protobuf';
import {timestampFromMs} from '@bufbuild/protobuf/wkt';
import type {ReactNode} from 'react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {lens} from './client';
import {useResourceMetricComparison} from './resourceMetricComparison';
import {GetLensInfoResponseSchema} from '../gen/natslens/v1/info_pb';
import {GetTimeSeriesBatchResponseSchema} from '../gen/natslens/v1/timeseries_pb';
import {ListStreamsResponseSchema} from '../gen/natslens/v1/streams_pb';
import {resourceMetric} from '../lib/resourceMetrics';
vi.mock('./client',()=>({lens:{getLensInfo:vi.fn(),listStreams:vi.fn(),getTimeSeriesBatch:vi.fn()}}));vi.mock('../lib/useSystem',()=>({useSystem:()=> 'example-prod'}));
const now=Date.parse('2026-10-17T14:30:00Z');const e={kind:'stream' as const,key:'APP/orders',name:'orders',account:'APP',stream:'orders',consumer:'',server:''};
beforeEach(()=>{vi.useFakeTimers();vi.mocked(lens.getLensInfo).mockResolvedValue(create(GetLensInfoResponseSchema,{now:timestampFromMs(now)}));vi.mocked(lens.getTimeSeriesBatch).mockImplementation(async req=>create(GetTimeSeriesBatchResponseSchema,{results:req.queries!.map(q=>({series:{metric:q.metric,entity:q.entity}}))}));vi.mocked(lens.listStreams).mockResolvedValue(create(ListStreamsResponseSchema,{streams:[],range:{start:timestampFromMs(now-3600000),end:timestampFromMs(now)},nextPageToken:'next',totalCount:13}));});
afterEach(()=>{vi.useRealTimers();vi.clearAllMocks()});
function mount(token='',selected=true,fixed=false){const qc=new QueryClient({defaultOptions:{queries:{retry:false,gcTime:Infinity}}});const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={qc}>{children}</QueryClientProvider>;return renderHook(()=>useResourceMetricComparison(fixed?{kind:'fixed',from:now-3600000,to:now}:{kind:'live',preset:'1h'},resourceMetric('stream-messages')!,selected?e:undefined,'APP','',token),{wrapper})}
async function settle(){await act(async()=>{await vi.advanceTimersByTimeAsync(1)})}
it('refreshes an explicitly selected live resource against the advancing source clock',async()=>{const q=mount();await settle();expect(q.result.current.data?.end).toBe(now);vi.mocked(lens.getLensInfo).mockResolvedValue(create(GetLensInfoResponseSchema,{now:timestampFromMs(now+30000)}));await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});expect(lens.getLensInfo).toHaveBeenCalledTimes(2);expect(q.result.current.data?.end).toBe(now+30000);q.unmount()});
it('refreshes the first live list page but keeps continuation pages pinned until reset',async()=>{const first=mount('',false);await settle();await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});expect(lens.listStreams).toHaveBeenCalledTimes(2);first.unmount();vi.mocked(lens.listStreams).mockClear();const next=mount('next',false);await settle();await act(async()=>{await vi.advanceTimersByTimeAsync(60000)});expect(lens.listStreams).toHaveBeenCalledTimes(1);expect(next.result.current.data?.end).toBe(now);next.unmount()});
it('does not advance explicitly fixed observations',async()=>{const q=mount('',true,true);await settle();await act(async()=>{await vi.advanceTimersByTimeAsync(60000)});expect(lens.getLensInfo).toHaveBeenCalledTimes(1);expect(q.result.current.data?.end).toBe(now);q.unmount()});

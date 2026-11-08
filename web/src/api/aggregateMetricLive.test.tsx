import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
import {act,renderHook} from '@testing-library/react';
import {create} from '@bufbuild/protobuf';
import {timestampFromMs} from '@bufbuild/protobuf/wkt';
import type {ReactNode} from 'react';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {lens} from './client';
import {useAggregateComparison} from './aggregateMetricComparison';
import {GetLensInfoResponseSchema} from '../gen/natslens/v1/info_pb';
import {GetTimeSeriesBatchResponseSchema} from '../gen/natslens/v1/timeseries_pb';
import {aggregateMetric} from '../lib/aggregateMetrics';
vi.mock('./client',()=>({lens:{getLensInfo:vi.fn(),getTimeSeriesBatch:vi.fn()}}));vi.mock('../lib/useSystem',()=>({useSystem:()=> 'example-prod'}));
const now=Date.parse('2026-10-17T14:30:00Z');
beforeEach(()=>{vi.useFakeTimers();vi.mocked(lens.getLensInfo).mockResolvedValue(create(GetLensInfoResponseSchema,{now:timestampFromMs(now)}));vi.mocked(lens.getTimeSeriesBatch).mockImplementation(async req=>create(GetTimeSeriesBatchResponseSchema,{results:req.queries!.map(q=>({series:{metric:q.metric}}))}));});
afterEach(()=>{vi.useRealTimers();vi.clearAllMocks()});
function mount(fixed=false){const qc=new QueryClient({defaultOptions:{queries:{retry:false,gcTime:Infinity}}});const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={qc}>{children}</QueryClientProvider>;return renderHook(()=>useAggregateComparison(fixed?{kind:'fixed',from:now-3600000,to:now}:{kind:'live',preset:'1h'},aggregateMetric('servers')!),{wrapper})}
it('refreshes live count observations from a fresh advancing source clock',async()=>{const q=mount();await act(async()=>{await vi.advanceTimersByTimeAsync(1)});expect(q.result.current.data?.end).toBe(now);vi.mocked(lens.getLensInfo).mockResolvedValue(create(GetLensInfoResponseSchema,{now:timestampFromMs(now+30000)}));await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});expect(q.result.current.data?.end).toBe(now+30000);q.unmount()});
it('retains fixed aggregate observations without a periodic refresh',async()=>{const q=mount(true);await act(async()=>{await vi.advanceTimersByTimeAsync(1)});await act(async()=>{await vi.advanceTimersByTimeAsync(60000)});expect(lens.getLensInfo).toHaveBeenCalledTimes(1);expect(q.result.current.data?.end).toBe(now);q.unmount()});

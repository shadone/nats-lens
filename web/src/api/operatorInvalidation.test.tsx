import {QueryClient,QueryClientProvider,useQueries} from '@tanstack/react-query';
import {act,render,waitFor} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {afterEach,expect,it,vi} from 'vitest';
import * as requests from './requests';
afterEach(()=>vi.clearAllMocks());
import {useMuteFinding,useUnmuteFinding,useSaveAcknowledgement} from './queries';
vi.mock('./requests',()=>({muteFinding:vi.fn(async()=>({})),unmuteFinding:vi.fn(async()=>({})),acknowledgeFinding:vi.fn(async()=>({findingId:'f1'})),endAcknowledgement:vi.fn(async()=>({findingId:'f1'}))}));
const dependent=['overview','shell','finding','findings','mutes','timeline','finding-episodes','stream','streams','consumer','consumers','serverInfo','serverList','applications','capacity','oncall','accounts','kvStores','objectStores','busiestConsumers','streamPlacement','currentFindings','server','streamNames','resource-metric-comparison','metric-comparison','future-status-view','lastData','replayStrip'];
const independent=['time-windows','series','trends','bandSeries','metric-ranking','lensInfo','data-history','event','events','serverEvents','timeline-events','captured-event-selection','replay','topology','placement'];
const input={system:'shop',checkCode:'stream-005',reason:'reviewed',expiresAt:Date.now()+3600000,mutedBy:'operator'};
function setup(awaitRefetch=true,holdFamily=''){
 const client=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity}}});
 const calls=new Map<string,ReturnType<typeof vi.fn>>();const releases:(()=>void)[]=[];
 const keys=[...dependent.map(f=>[f,'shop','fixed-history']),...independent.map(f=>[f,'shop','fixed-history']),...dependent.map(f=>[f,'other','fixed-history']),['systemSummaries','fixed-history']];
 const queries=keys.map(queryKey=>{let count=0;const name=queryKey.join(':');const queryFn=vi.fn(async()=>{count++;if(count>1&&queryKey[0]===holdFamily)await new Promise<void>(r=>releases.push(r));return count});calls.set(name,queryFn);return {queryKey,queryFn}});
 let actions: {mute:ReturnType<typeof useMuteFinding>;unmute:ReturnType<typeof useUnmuteFinding>;ack:ReturnType<typeof useSaveAcknowledgement>};
 function Hooks(){useQueries({queries});actions={mute:useMuteFinding({awaitRefetch}),unmute:useUnmuteFinding(),ack:useSaveAcknowledgement()};return null}
 render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/findings?system=other']}><Hooks/></MemoryRouter></QueryClientProvider>);
 return {client,calls,initial:keys.length,ready:()=>actions!,release:()=>releases.splice(0).forEach(f=>f())};
}
it('refreshes captured-system operator evidence and global tallies without refetching raw data or another system',async()=>{
 const h=setup();await waitFor(()=>expect([...h.calls.values()].every(f=>f.mock.calls.length===1)).toBe(true));
 await act(async()=>{await h.ready().mute.mutateAsync(input)});
 expect([...h.calls.values()].reduce((n,f)=>n+f.mock.calls.length,0)).toBe(h.initial+dependent.length+1);
 for(const f of dependent)expect(h.calls.get(`${f}:shop:fixed-history`),f).toHaveBeenCalledTimes(2);
 expect(h.calls.get('systemSummaries:fixed-history')).toHaveBeenCalledTimes(2);
 for(const f of independent)expect(h.calls.get(`${f}:shop:fixed-history`),f).toHaveBeenCalledTimes(1);
 for(const f of dependent)expect(h.calls.get(`${f}:other:fixed-history`),f).toHaveBeenCalledTimes(1);
 await act(async()=>{await h.ready().unmute.mutateAsync({system:'shop',muteId:'m1',unmutedBy:'operator'})});
 for(const f of dependent)expect(h.calls.get(`${f}:shop:fixed-history`),f).toHaveBeenCalledTimes(3);
 for(const f of independent)expect(h.calls.get(`${f}:shop:fixed-history`),f).toHaveBeenCalledTimes(1);
});
it('does not wait for an unrelated blocked series refresh',async()=>{
 const h=setup(true,'series');await waitFor(()=>expect(h.calls.get('series:shop:fixed-history')).toHaveBeenCalledTimes(1));
 try{act(()=>{h.ready().mute.mutate(input)});await waitFor(()=>expect(h.ready().mute.isSuccess).toBe(true),{timeout:500})}finally{await act(async()=>h.release())}
});
it('preserves the explicit nonwaiting acceptance option while relevant evidence refreshes',async()=>{
 const h=setup(false,'overview');await waitFor(()=>expect(h.calls.get('overview:shop:fixed-history')).toHaveBeenCalledTimes(1));
 try{await act(async()=>{await h.ready().mute.mutateAsync(input)});expect(h.ready().mute.isSuccess).toBe(true);expect(h.calls.get('overview:shop:fixed-history')).toHaveBeenCalledTimes(2)}finally{await act(async()=>h.release())}
});

it('invalidates inactive historical operator reads but leaves other systems and raw history fresh',async()=>{
 const h=setup();await waitFor(()=>expect(h.calls.get('overview:shop:fixed-history')).toHaveBeenCalledTimes(1));
 const target=['finding-episodes','shop','inactive-history'],other=['finding-episodes','other','inactive-history'],raw=['series','shop','inactive-history'];
 for(const key of [target,other,raw])h.client.setQueryData(key,{value:1});
 await act(async()=>{await h.ready().mute.mutateAsync(input)});
 expect(h.client.getQueryState(target)?.isInvalidated).toBe(true);expect(h.client.getQueryState(other)?.isInvalidated).toBe(false);expect(h.client.getQueryState(raw)?.isInvalidated).toBe(false);
});
it('acknowledgement writes use the same scoped dependencies without invalidating series',async()=>{
 const h=setup();await waitFor(()=>expect(h.calls.get('overview:shop:fixed-history')).toHaveBeenCalledTimes(1));
 await act(async()=>{await h.ready().ack.mutateAsync({system:'shop',action:'ack',input:{findingId:'f1',note:'checking',ackedBy:'operator'}})});
 await waitFor(()=>expect(h.calls.get('overview:shop:fixed-history')).toHaveBeenCalledTimes(2));
 expect(h.calls.get('systemSummaries:fixed-history')).toHaveBeenCalledTimes(2);expect(h.calls.get('series:shop:fixed-history')).toHaveBeenCalledTimes(1);expect(h.calls.get('overview:other:fixed-history')).toHaveBeenCalledTimes(1);
});
it('failed writes retain error state and do not refresh caches',async()=>{
 const h=setup();await waitFor(()=>expect([...h.calls.values()].every(f=>f.mock.calls.length===1)).toBe(true));
 vi.mocked(requests.muteFinding).mockRejectedValueOnce(new Error('fixture write rejected'));
 await act(async()=>{await expect(h.ready().mute.mutateAsync(input)).rejects.toThrow('fixture write rejected')});
 await waitFor(()=>expect(h.ready().mute.isError).toBe(true));expect([...h.calls.values()].every(f=>f.mock.calls.length===1)).toBe(true);
});
it('the nonwaiting option still waits for the captured-system write itself',async()=>{
 const h=setup(false);await waitFor(()=>expect(h.calls.get('overview:shop:fixed-history')).toHaveBeenCalledTimes(1));
 let finish:()=>void=()=>{};vi.mocked(requests.muteFinding).mockImplementationOnce(()=>new Promise(resolve=>{finish=()=>resolve({} as Awaited<ReturnType<typeof requests.muteFinding>>)}));
 act(()=>h.ready().mute.mutate(input));await waitFor(()=>expect(h.ready().mute.isPending).toBe(true));
 expect(h.calls.get('overview:shop:fixed-history')).toHaveBeenCalledTimes(1);
 await act(async()=>finish());await waitFor(()=>expect(h.ready().mute.isSuccess).toBe(true));
 expect(vi.mocked(requests.muteFinding).mock.calls[0][0]).toBe('shop');
});

it('refreshes acknowledgement event feeds and event coverage without touching another system',async()=>{
 const h=setup();await waitFor(()=>expect(h.calls.get('events:shop:fixed-history')).toHaveBeenCalledTimes(1));
 await act(async()=>{await h.ready().ack.mutateAsync({system:'shop',action:'ack',input:{findingId:'f1',note:'checking',ackedBy:'operator'}})});
 await waitFor(()=>expect(h.calls.get('events:shop:fixed-history')).toHaveBeenCalledTimes(2));
 expect(h.calls.get('serverEvents:shop:fixed-history')).toHaveBeenCalledTimes(2);expect(h.calls.get('data-history:shop:fixed-history')).toHaveBeenCalledTimes(2);expect(h.calls.get('event:shop:fixed-history')).toHaveBeenCalledTimes(1);
});

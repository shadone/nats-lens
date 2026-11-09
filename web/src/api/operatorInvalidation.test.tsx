import {QueryClient,QueryClientProvider,useQueries} from '@tanstack/react-query';
import {act,render,waitFor} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {expect,it,vi} from 'vitest';
import {useMuteFinding,useUnmuteFinding} from './queries';
vi.mock('./requests',()=>({muteFinding:vi.fn(async()=>({})),unmuteFinding:vi.fn(async()=>({}))}));
const dependent=['overview','shell','finding','findings','mutes','timeline','finding-episodes','stream','streams','consumer','consumers','serverInfo','serverList','applications','capacity','oncall'];
const independent=['series','trends','bandSeries','metric-ranking','lensInfo','data-history','lastData','event','events','serverEvents','timeline-events','captured-event-selection','replay','replayStrip','topology','placement'];
const input={system:'shop',checkCode:'stream-005',reason:'reviewed',expiresAt:Date.now()+3600000,mutedBy:'operator'};
function setup(awaitRefetch=true,holdFamily=''){
 const client=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:Infinity}}});
 const calls=new Map<string,ReturnType<typeof vi.fn>>();const releases:(()=>void)[]=[];
 const keys=[...dependent.map(f=>[f,'shop','fixed-history']),...independent.map(f=>[f,'shop','fixed-history']),...dependent.map(f=>[f,'other','fixed-history']),['systemSummaries','fixed-history']];
 const queries=keys.map(queryKey=>{let count=0;const name=queryKey.join(':');const queryFn=vi.fn(async()=>{count++;if(count>1&&queryKey[0]===holdFamily)await new Promise<void>(r=>releases.push(r));return count});calls.set(name,queryFn);return {queryKey,queryFn}});
 let actions: {mute:ReturnType<typeof useMuteFinding>;unmute:ReturnType<typeof useUnmuteFinding>};
 function Hooks(){useQueries({queries});actions={mute:useMuteFinding({awaitRefetch}),unmute:useUnmuteFinding()};return null}
 render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/findings?system=other']}><Hooks/></MemoryRouter></QueryClientProvider>);
 return {client,calls,ready:()=>actions!,release:()=>releases.splice(0).forEach(f=>f())};
}
it('refreshes captured-system operator evidence and global tallies without refetching raw data or another system',async()=>{
 const h=setup();await waitFor(()=>expect([...h.calls.values()].every(f=>f.mock.calls.length===1)).toBe(true));
 await act(async()=>{await h.ready().mute.mutateAsync(input)});
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

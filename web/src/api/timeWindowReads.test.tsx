import {focusManager,QueryClient,QueryClientProvider} from '@tanstack/react-query';
import {act,render,waitFor} from '@testing-library/react';
import {MemoryRouter,useNavigate} from 'react-router';
import {afterEach,expect,it,vi} from 'vitest';
import {useTimeWindows} from './queries';
import * as requests from './requests';
import type {RangeSpec} from '../lib/timeRange';
vi.mock('./requests',()=>({getTimeWindows:vi.fn()}));
afterEach(()=>vi.clearAllMocks());
it('shares settled annotations and isolates system/range changes without placeholder evidence',async()=>{
 const request=vi.mocked(requests.getTimeWindows);
 let release:(value:[])=>void=()=>{};
 request.mockResolvedValueOnce([{start:1,end:2,label:'old',detail:''}]).mockImplementation(()=>new Promise(r=>{release=r}));
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 let navigate:ReturnType<typeof useNavigate>=()=>{};const seen:(unknown)[]=[];
 function Reader({spec}:{spec:RangeSpec}){navigate=useNavigate();const q=useTimeWindows(spec);seen.push(q.data);return null}
 function App({extra=false,to=2}:{extra?:boolean;to?:number}){return <QueryClientProvider client={client}><MemoryRouter initialEntries={['/?system=shop']}><Reader spec={{kind:'fixed',from:1,to}}/>{extra&&<Reader spec={{kind:'fixed',from:1,to}}/>}</MemoryRouter></QueryClientProvider>}
 const view=render(<App/>);await waitFor(()=>expect(seen.at(-1)).toEqual([{start:1,end:2,label:'old',detail:''}]));
 view.rerender(<App extra/>);expect(request).toHaveBeenCalledTimes(1);
 act(()=>navigate('/?system=other'));expect(seen.at(-1)).toBeUndefined();await waitFor(()=>expect(request).toHaveBeenCalledTimes(2));
 const signal=request.mock.calls[1][2];expect(signal).toBeInstanceOf(AbortSignal);
 view.rerender(<App extra to={3}/>);await waitFor(()=>expect(request).toHaveBeenCalledTimes(3));expect(signal?.aborted).toBe(true);expect(seen.at(-1)).toBeUndefined();
 await act(async()=>release([]));view.unmount();client.clear();
});
it('polls only live annotations and shares their freshness interval',async()=>{
 vi.mocked(requests.getTimeWindows).mockResolvedValue([]);
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 function Read(){useTimeWindows({kind:'live',preset:'1h'});useTimeWindows({kind:'fixed',from:1,to:2});return null}
 const view=render(<QueryClientProvider client={client}><MemoryRouter><Read/></MemoryRouter></QueryClientProvider>);
 await waitFor(()=>expect(requests.getTimeWindows).toHaveBeenCalledTimes(2));
 const queries=client.getQueryCache().getAll();
 for(const query of queries){const options=[...query.observers][0]?.options;expect(options?.refetchInterval).toBe(String(query.queryKey[2]).startsWith('fixed:')?false:30000)}
 view.unmount();client.clear();
});

it('issues one shared live read per poll and no fixed-range polls',async()=>{
 vi.useFakeTimers();focusManager.setFocused(true);
 vi.mocked(requests.getTimeWindows).mockResolvedValue([]);
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 function Read(){useTimeWindows({kind:'live',preset:'1h'});useTimeWindows({kind:'live',preset:'1h'});useTimeWindows({kind:'fixed',from:1,to:2});return null}
 const view=render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/?system=shop']}><Read/></MemoryRouter></QueryClientProvider>);
 try{
  await act(async()=>{await vi.advanceTimersByTimeAsync(0)});
  expect(requests.getTimeWindows).toHaveBeenCalledTimes(2);
  for(let i=0;i<3;i++)await act(async()=>{await vi.advanceTimersByTimeAsync(30000)});
  const calls=vi.mocked(requests.getTimeWindows).mock.calls;
  expect(calls.filter(([,spec])=>spec.kind==='live')).toHaveLength(4);
  expect(calls.filter(([,spec])=>spec.kind==='fixed')).toHaveLength(1);
  expect(calls.every(([system])=>system==='shop')).toBe(true);
 }finally{view.unmount();client.clear();focusManager.setFocused(undefined);vi.useRealTimers()}
});

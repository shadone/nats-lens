import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
import {act,renderHook,waitFor} from '@testing-library/react';
import type {ReactNode} from 'react';
import {MemoryRouter} from 'react-router';
import {expect,it,vi} from 'vitest';
import {useSeries} from './queries';
vi.mock('./client',()=>({lens:new Proxy({}, {get:()=>()=>new Promise(()=>{})})}));
it('does not lend one resource history to another and retains requested identity while loading',async()=>{const qc=new QueryClient({defaultOptions:{queries:{retry:false}}});const e={kind:'stream' as const,key:'APP/orders',name:'orders',account:'APP',stream:'orders',consumer:'',server:''};const wrapper=({children}:{children:ReactNode})=><QueryClientProvider client={qc}><MemoryRouter>{children}</MemoryRouter></QueryClientProvider>;const {result,rerender}=renderHook(({entity})=>useSeries({kind:'live',preset:'1h'},'stream.messages',entity),{wrapper,initialProps:{entity:e}});act(()=>{for(const q of qc.getQueryCache().findAll())qc.setQueryData(q.queryKey,{metric:'stream.messages',entity:e,points:[]})});await waitFor(()=>expect(result.current.data).toBeDefined());const other={...e,key:'APP/refunds',stream:'refunds',name:'refunds'};rerender({entity:other});expect(result.current.data).toBeUndefined();expect((result.current as unknown as {metric:string;entity:typeof e}).metric).toBe('stream.messages');expect((result.current as unknown as {entity:typeof e}).entity.key).toBe(other.key)});

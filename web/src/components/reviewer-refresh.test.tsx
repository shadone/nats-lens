import {QueryClient, QueryClientProvider} from '@tanstack/react-query';
import {fireEvent, render, screen, waitFor} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {expect,it,vi} from 'vitest';
import {useStreams} from '../api/queries';
import {lens} from '../api/client';
import {ListTraversal,useListTraversal} from './ListTraversal';
vi.mock('../api/client',()=>({lens:{listStreams:vi.fn()}}));
function Page(){
 const t=useListTraversal('same-scope');
 const q=useStreams({kind:'live',preset:'1h'},false,t.token);
 return <ListTraversal traversal={t} next={q.data?.nextPageToken} loaded={0} error={q.error} refresh={()=>{if(!t.token)void q.refetch();}}/>;
}
it('Refresh list starts a fresh request from a continuation even while page one is fresh in cache',async()=>{
 vi.mocked(lens.listStreams).mockImplementation(async(req)=>({streams:[],totals:{},totalCount:200,nextPageToken:req.pageToken?'':'next'} as never));
 const qc=new QueryClient({defaultOptions:{queries:{retry:false,staleTime:10000,refetchOnWindowFocus:false}}});
 render(<QueryClientProvider client={qc}><MemoryRouter><Page/></MemoryRouter></QueryClientProvider>);
 await waitFor(()=>expect(screen.getByRole('button',{name:'Next page'})).toBeEnabled());
 fireEvent.click(screen.getByRole('button',{name:'Next page'}));
 await waitFor(()=>expect(lens.listStreams).toHaveBeenCalledTimes(2));
 fireEvent.click(screen.getByRole('button',{name:'Refresh list'}));
 await waitFor(()=>expect(lens.listStreams).toHaveBeenCalledTimes(3));
 qc.clear();
});

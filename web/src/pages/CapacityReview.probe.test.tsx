import { useState } from 'react';
import { expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { renderApp } from '../test/render';
import { CapacityHistoryChart } from '../components/CapacityHistory';
import { ServerCards } from '../components/CapacityServers';
import { prod } from '../test/fixtures/capacity';
vi.mock('../api/client', () => ({lens:{getTimeSeriesBatch:vi.fn(() => new Promise(() => {}))}}));
it('does not draw a measured line across a missing file-store bucket', () => {
 const start=1792238400000, step=30000;
 const q={isPending:false,error:undefined,data:{metric:'cluster.capacity_file_store_bytes',unit:'bytes' as const,stepMs:step,points:[{time:start,value:100},{time:start+2*step,value:100}],topPoints:[]}};
 const v=renderApp(<CapacityHistoryChart q={q} title="Total JetStream file-store usage" />, '/capacity?from=2026-10-17T12%3A00%3A00Z&to=2026-10-17T12%3A02%3A00Z');
 expect(v.container.querySelectorAll('.tc-svg polyline')).toHaveLength(0);
});
it('previous server page works after the server dataset shrinks', async () => {
 const servers=Array.from({length:80},(_,i)=>({...prod().servers[0],name:`nats-${i}`}));
 function C() {const [short,setShort]=useState(false);return <><button onClick={()=>setShort(true)}>Shrink</button><ServerCards servers={short?servers.slice(0,25):servers} withoutJetstream={0}/></>}
 renderApp(<C/>,'/capacity');
 const next=screen.getByRole('button',{name:'Next servers'});
 fireEvent.click(next);fireEvent.click(next);fireEvent.click(next);
 expect(screen.getByText(/Server cards 73–80/)).toBeInTheDocument();
 fireEvent.click(screen.getByRole('button',{name:'Shrink'}));
 expect(screen.getByText(/Server cards 25–25/)).toBeInTheDocument();
 fireEvent.click(screen.getByRole('button',{name:'Previous servers'}));
 expect(screen.getByText(/Server cards 1–24/)).toBeInTheDocument();
});

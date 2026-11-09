import {fireEvent,render,screen} from '@testing-library/react';
import {MemoryRouter,Route,Routes} from 'react-router';
import {it,expect,vi} from 'vitest';
import {TimelineEvents} from './TimelineEvents';
import {selectionPath,type CapturedSelection} from '../lib/capturedEventSelection';
import {useCapturedSelectionPage} from '../api/timelineEvidence';
vi.mock('../api/timelineEvidence',()=>({useCapturedSelectionPage:vi.fn(),useCapturedEvent:()=>({data:undefined,isPending:true,error:null})}));
const selection:CapturedSelection={version:1,system:'shop',from:'2026-10-17T12:00:00Z',to:'2026-10-17T13:00:00Z',eventReadId:'99',ids:Array.from({length:51},(_,i)=>`${i+1}`),limits:['finding transitions']};
it('reopens exact membership, discloses missing rows and navigates explicit frozen pages',()=>{
 vi.mocked(useCapturedSelectionPage).mockReturnValue({data:{items:[],totalCount:0},isPending:false,error:null} as unknown as ReturnType<typeof useCapturedSelectionPage>);
 render(<MemoryRouter initialEntries={[selectionPath(selection)]}><Routes><Route path="/timeline/events" element={<TimelineEvents/>}/></Routes></MemoryRouter>);
 expect(screen.getByText(/51 captured event identities/)).toBeInTheDocument();expect(screen.getByRole('note')).toHaveTextContent('finding transitions');expect(screen.getAllByRole('button',{name:'Read event detail'})).toHaveLength(50);expect(screen.getAllByText(/Recorded row unavailable/)).toHaveLength(50);
 fireEvent.click(screen.getByRole('link',{name:'Next captured events'}));expect(useCapturedSelectionPage).toHaveBeenLastCalledWith(selection,50,50);expect(screen.getAllByRole('button',{name:'Read event detail'})).toHaveLength(1);
});
it('does not query when the selection link is missing or its system was changed',()=>{
 vi.mocked(useCapturedSelectionPage).mockClear();const u=new URL(selectionPath(selection),'https://example.com');u.searchParams.set('system','other');
 render(<MemoryRouter initialEntries={[u.pathname+u.search+u.hash]}><TimelineEvents/></MemoryRouter>);expect(screen.getByRole('alert')).toHaveTextContent('captured selection');expect(useCapturedSelectionPage).not.toHaveBeenCalled();
});

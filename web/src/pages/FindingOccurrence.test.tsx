import {fireEvent,render,screen} from '@testing-library/react';
import {MemoryRouter,Routes,Route} from 'react-router';
import {expect,it,vi} from 'vitest';
import {FindingOccurrence} from './FindingOccurrence';
import {useFindingEpisodes} from '../api/findingEpisodes';
import {InvestigationHandoff} from '../components/InvestigationContext';
vi.mock('../api/findingEpisodes',async importOriginal=>({...await importOriginal<typeof import('../api/findingEpisodes')>(),useFindingEpisodes:vi.fn()}));
const from='2026-10-17T12:00:00Z',to='2026-10-17T13:00:00Z';
const finding={id:'stream-001:APP/orders',checkCode:'stream-001',checkName:'Replicas',severity:'warning',state:'firing',detail:'Old episode',remediation:'Current advice',transitionsLastHour:0};
function mount(tail=''){return render(<MemoryRouter initialEntries={[`/findings/stream-001%3AAPP%2Forders/occurrences/9007199254740993?system=shop&from=${from}&to=${to}&readEpoch=9007199254740995${tail}`]}><Routes><Route path="/findings/:id/occurrences/:firstEpoch" element={<FindingOccurrence/>}/></Routes></MemoryRouter>)}
it('opens exact historical evidence and continues only its bounded raw history',()=>{
 vi.mocked(useFindingEpisodes).mockReturnValue({data:{items:[{finding,firstEpoch:'9007199254740993',startEpoch:'9007199254740993',firstAt:Date.parse(from),latestState:'resolved',latestAt:Date.parse(to),transitions:[{epoch:'9007199254740993',state:'firing',severity:'warning',detail:'Recorded old detail',time:Date.parse(from)}],transitionCount:101,nextTransitionOffset:100,transitionToken:'raw-next',unavailable:['historical_operator_state'],causeEventId:'42'}],readEpoch:'9007199254740995'},isPending:false,error:null} as unknown as ReturnType<typeof useFindingEpisodes>);
 mount();
 expect(screen.getByText('Old episode')).toBeInTheDocument();expect(screen.getByText('Recorded old detail')).toBeInTheDocument();
 expect(useFindingEpisodes).toHaveBeenLastCalledWith(Date.parse(from),Date.parse(to),expect.objectContaining({findingId:'stream-001:APP/orders',firstEpoch:'9007199254740993',readEpoch:'9007199254740995',includeTransitions:true,pageSize:1}));
 fireEvent.click(screen.getByRole('button',{name:'Older transitions'}));
 expect(useFindingEpisodes).toHaveBeenLastCalledWith(Date.parse(from),Date.parse(to),expect.objectContaining({pageToken:'raw-next',transitionOffset:100,firstEpoch:'9007199254740993'}));
 expect(screen.getByRole('link',{name:'Open latest finding details'}).getAttribute('href')).toContain('system=shop');
 expect(screen.queryByRole('button',{name:/Mute|Acknowledge/})).not.toBeInTheDocument();
});
it('does not replace an expired occurrence with current details',()=>{
 vi.mocked(useFindingEpisodes).mockReturnValue({data:{items:[],readEpoch:'5'},isPending:false,error:null} as unknown as ReturnType<typeof useFindingEpisodes>);mount();
 expect(screen.getByText(/No later occurrence has been substituted/)).toBeInTheDocument();
});
it('preserves fractional UTC bounds in reads, pagination, links and copied evidence',()=>{
 const range={start:'2026-10-17T12:00:00.000001Z',end:'2026-10-17T13:00:00.000999Z'},handoff=vi.fn();
 vi.mocked(useFindingEpisodes).mockReturnValue({data:{items:[{finding,firstEpoch:'1',firstAt:Date.parse(from),latestState:'firing',transitions:[],transitionCount:101,nextTransitionOffset:100,transitionToken:'next',unavailable:[],causeEventId:'0'}],readEpoch:'5'},isPending:false,error:null} as unknown as ReturnType<typeof useFindingEpisodes>);
 render(<MemoryRouter initialEntries={[`/findings/f/occurrences/1?system=shop&from=${range.start}&to=${range.end}&readEpoch=5`]}><InvestigationHandoff.Provider value={handoff}><Routes><Route path="/findings/:id/occurrences/:firstEpoch" element={<FindingOccurrence/>}/></Routes></InvestigationHandoff.Provider></MemoryRouter>);
 expect(useFindingEpisodes).toHaveBeenLastCalledWith(Date.parse(range.start),Date.parse(range.end),expect.objectContaining({range}));
 fireEvent.click(screen.getByRole('button',{name:'Older transitions'}));
 expect(useFindingEpisodes).toHaveBeenLastCalledWith(Date.parse(range.start),Date.parse(range.end),expect.objectContaining({range,pageToken:'next',transitionOffset:100}));
 const link=new URL(screen.getByRole('link',{name:'Open latest finding details'}).getAttribute('href')!,'https://example.com');expect(link.searchParams.get('from')).toBe(range.start);expect(link.searchParams.get('to')).toBe(range.end);
 fireEvent.click(screen.getByRole('button',{name:'Copy as prompt'}));
 expect(handoff).toHaveBeenCalledWith(expect.objectContaining({range,query:expect.objectContaining({request:expect.objectContaining({range})})}),expect.any(Function));
});

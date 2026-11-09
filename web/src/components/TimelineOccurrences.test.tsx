import {fireEvent,render,screen} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {expect,it,vi} from 'vitest';
import {TimelineFindingRange} from './TimelineInvestigation';
import {useFindingEpisodes} from '../api/findingEpisodes';
vi.mock('../lib/useGlobalQuery',()=>({useGlobalQuery:()=>'?system=demo'}));
vi.mock('../api/findingEpisodes',()=>({useFindingEpisodes:vi.fn()}));
vi.mock('../api/queries',()=>({useFindings:()=>({data:{items:[],totalCount:0,nextPageToken:''},isPending:false,error:null})}));
const from=Date.parse('2026-10-17T12:00:00Z'),end=from+3600000;
const f={id:'stream-001:APP/orders',checkCode:'stream-001',checkName:'Stream replicas',severity:'warning',state:'firing',detail:'Earlier recorded evidence',transitionsLastHour:0};
const episode=(firstEpoch:string)=>({finding:f,firstEpoch,startEpoch:firstEpoch,firstAt:from,latestState:'resolved',latestSeverity:'warning',latestAt:end,transitions:[],transitionCount:2,nextTransitionOffset:0,transitionToken:'',unavailable:[]});
it('keeps repeated occurrences separate and pins raw-history drilldown to its exact identity',()=>{
 vi.mocked(useFindingEpisodes).mockImplementation((_from,_end,opts)=>({data:{items:[episode('9007199254740993'),episode('9007199254740995')],totalCount:51,nextPageToken:opts.pageToken?'':'next',readEpoch:'9007199254741000'},isPending:false,error:null}) as unknown as ReturnType<typeof useFindingEpisodes>);
 render(<MemoryRouter initialEntries={['/?system=demo']}><TimelineFindingRange from={from} end={end} checkCode="stream-001" readEpoch="9007199254741000"/></MemoryRouter>);
 expect(screen.getAllByText('Earlier recorded evidence')).toHaveLength(2);
 expect(useFindingEpisodes).toHaveBeenLastCalledWith(from,end,expect.objectContaining({checkCode:'stream-001',readEpoch:'9007199254741000',pageSize:50}));
 const links=screen.getAllByRole('link',{name:'Open this occurrence'});
 expect(links[0].getAttribute('href')).toContain('9007199254740993');expect(links[1].getAttribute('href')).toContain('9007199254740995');
 expect(links[0].getAttribute('href')).toContain('readEpoch=9007199254741000');
 expect(screen.getAllByText(/Latest retained state: Resolved/)).toHaveLength(2);
 fireEvent.click(screen.getByRole('button',{name:'Next'}));
 expect(useFindingEpisodes).toHaveBeenLastCalledWith(from,end,expect.objectContaining({pageToken:'next',readEpoch:'9007199254741000'}));
});
it('labels missing occurrence history as unavailable, not healthy',()=>{
 vi.mocked(useFindingEpisodes).mockReturnValue({data:{items:[],totalCount:0,nextPageToken:'',readEpoch:'0'},isPending:false,error:null} as unknown as ReturnType<typeof useFindingEpisodes>);
 render(<MemoryRouter><TimelineFindingRange from={from} end={end} readEpoch="0"/></MemoryRouter>);
 expect(useFindingEpisodes).toHaveBeenCalled();
 expect(screen.getByText(/Evidence may have expired or not been captured/)).toBeInTheDocument();
});

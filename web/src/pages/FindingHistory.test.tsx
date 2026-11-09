import {render,screen} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {beforeEach,expect,it,vi} from 'vitest';
import {FindingHistory} from './FindingHistory';
import {useFindingEpisodes} from '../api/findingEpisodes';
vi.mock('../api/findingEpisodes',async original=>({...await original<typeof import('../api/findingEpisodes')>(),useFindingEpisodes:vi.fn()}));
const from='2026-10-17T12:00:00Z',to='2026-10-17T13:00:00Z';
const f={id:'stream-001:APP/orders',checkCode:'stream-001',checkName:'Replicas',severity:'warning' as const,state:'firing' as const,detail:'Recorded occurrence',remediation:'Current advice',transitionsLastHour:0};
beforeEach(()=>vi.clearAllMocks());
it('lists separate retained recurrences with its captured boundary',()=>{
 vi.mocked(useFindingEpisodes).mockReturnValue({data:{items:[{finding:f,firstEpoch:'1',firstAt:Date.parse(from),latestState:'resolved',transitions:[]},{finding:f,firstEpoch:'3',firstAt:Date.parse(from)+60000,latestState:'firing',transitions:[]}],readEpoch:'9007199254740995',totalCount:2,nextPageToken:''},isPending:false,error:null} as unknown as ReturnType<typeof useFindingEpisodes>);
 render(<MemoryRouter initialEntries={[`/findings/history?system=shop&from=${from}&to=${to}&readEpoch=9007199254740995&check=stream-001&type=stream&key=APP%2Forders`]}><FindingHistory/></MemoryRouter>);
 expect(screen.getByRole('heading',{name:'Finding occurrence history'})).toBeInTheDocument();expect(screen.getAllByRole('link',{name:'Open this occurrence'})).toHaveLength(2);
 expect(useFindingEpisodes).toHaveBeenLastCalledWith(Date.parse(from),Date.parse(to),expect.objectContaining({readEpoch:'9007199254740995',checkCode:'stream-001',entity:expect.objectContaining({kind:'stream',key:'APP/orders'}),pageSize:50}));
 const hrefs=screen.getAllByRole('link',{name:'Open this occurrence'}).map(a=>a.getAttribute('href'));expect(hrefs[0]).toContain('/occurrences/1?');expect(hrefs[1]).toContain('/occurrences/3?');
});
it('rejects missing fixed bounds and invalid typed scopes without querying broader history',()=>{
 render(<MemoryRouter initialEntries={[`/findings/history?system=shop&from=${from}&to=${to}&type=unknown&key=APP%2Forders`]}><FindingHistory/></MemoryRouter>);
 expect(screen.getByRole('alert')).toHaveTextContent(/valid fixed interval and supported resource scope/i);expect(useFindingEpisodes).not.toHaveBeenCalled();
});

it('keeps a known empty boundary without querying the current capture',()=>{
 vi.mocked(useFindingEpisodes).mockReturnValue({data:{items:[],readEpoch:'0',totalCount:0,nextPageToken:''},isPending:false,error:null} as unknown as ReturnType<typeof useFindingEpisodes>);
 render(<MemoryRouter initialEntries={[`/findings/history?system=shop&from=${from}&to=${to}&readEpoch=0`]}><FindingHistory/></MemoryRouter>);expect(screen.getByText(/No retained occurrences match/)).toBeInTheDocument();expect(useFindingEpisodes).toHaveBeenLastCalledWith(Date.parse(from),Date.parse(to),expect.objectContaining({readEpoch:'0'}));
});

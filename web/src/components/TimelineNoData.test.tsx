import {fireEvent,render,screen,within} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {expect,it,vi} from 'vitest';
import type {Timeline,Level} from '../api/model';
import {StoryLanes} from './StoryLanes';
vi.mock('../api/findingEpisodes',()=>({useFindingEpisodes:()=>({data:{items:[],totalCount:0,nextPageToken:'',readEpoch:'0'},isPending:false,error:null})}));
const start=Date.parse('2026-10-17T23:59:00Z');
const make=(cells:Level[]):Timeline=>({start,bucketMs:60000,bucketCount:cells.length,rows:[{key:'server-001',checkCode:'server-001',checkName:'Server reporting',severity:'critical',cells,now:'nodata'}],kubernetes:[],nats:[],windows:[],findingReadEpoch:'0',natsTruncated:false,findingTruncated:false,kubernetesTruncated:false});
function show(tl:Timeline){return render(<MemoryRouter initialEntries={['/?system=shop']}><StoryLanes tl={tl} end={start+120000} live={false}/></MemoryRouter>)}
it('keeps an all-missing finding lane inspectable without calling it healthy or zero failures',()=>{
 show(make(['nodata','nodata']));const lane=screen.getByRole('group',{name:'Findings: no observed check state'});expect(lane).not.toHaveAccessibleName(/ok throughout/i);expect(screen.getByText('no observed state')).toBeInTheDocument();expect(screen.queryByText('0 checks fired')).not.toBeInTheDocument();const mark=within(lane).getByRole('button',{name:'Finding state: No data'});fireEvent.focus(mark);expect(screen.getByRole('tooltip')).toHaveTextContent('No check state was observed in these buckets');fireEvent.click(mark);expect(screen.getByRole('dialog')).toHaveTextContent('Evidence may have expired or not been captured');
});
it('keeps missing runs beside observed warnings across midnight',()=>{
 show(make(['warning','nodata']));const lane=screen.getByRole('group',{name:/Findings: warning.*no data/});expect(within(lane).getByRole('button',{name:'Finding state: Warning'})).toBeInTheDocument();const mark=within(lane).getByRole('button',{name:'Finding state: No data'});fireEvent.focus(mark);expect(screen.getByRole('tooltip')).toHaveTextContent('2026-10-18 00:00:00 UTC');expect(screen.getByRole('tooltip')).not.toHaveTextContent('server-001: Server reporting');
});
it('describes observed OK buckets without claiming complete evaluation or capture',()=>{
 show(make(['ok','ok']));expect(screen.getByRole('group',{name:'Findings: OK in observed buckets'})).toBeInTheDocument();expect(screen.queryByRole('button',{name:'Finding state: No data'})).not.toBeInTheDocument();expect(screen.getByText('0 checks fired')).toBeInTheDocument();
});
it('does not invent healthy state or bucket geometry without any returned buckets',()=>{
 show({...make([]),rows:[],bucketCount:0});expect(screen.getByRole('group',{name:'Findings: no observed check state'})).toBeInTheDocument();expect(screen.queryByRole('button',{name:'Finding state: No data'})).not.toBeInTheDocument();
});
it('continues to aggregate the worst observed state when only another check is missing',()=>{
 const tl=make(['ok']);tl.rows.push({...tl.rows[0],key:'other',checkCode:'server-002',cells:['nodata']});show(tl);expect(screen.getByRole('group',{name:'Findings: OK in observed buckets'})).toBeInTheDocument();expect(screen.queryByRole('button',{name:'Finding state: No data'})).not.toBeInTheDocument();
});

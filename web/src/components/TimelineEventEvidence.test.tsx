import {fireEvent,render,screen} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {expect,it,vi} from 'vitest';
import type {UseQueryResult} from '@tanstack/react-query';
const result=<T,>(data:T)=>({data,isPending:false,error:null} as UseQueryResult<T,Error>);
import type {Timeline} from '../api/model';
import {StoryLanes} from './StoryLanes';
import {useCapturedEvent,useSelectedTimelineEvents} from '../api/timelineEvidence';
vi.mock('../lib/useWidth',()=>({useWidth:()=>800}));
vi.mock('../lib/useGlobalQuery',()=>({useGlobalQuery:()=>'?system=shop&account=APP&range=1h'}));
vi.mock('../api/timelineEvidence',()=>({useCapturedEvent:vi.fn(),useSelectedTimelineEvents:vi.fn()}));
vi.mock('../api/queries',()=>({useFindings:()=>({data:{items:[],totalCount:0,nextPageToken:''},isPending:false,error:null})}));
const start=Date.parse('2026-10-17T23:59:00Z'),end=start+120000;
const range={start:'2026-10-17T23:59:00Z',end:'2026-10-18T00:01:00Z'};
const e={id:'9007199254740993',eventId:'9007199254740993',time:start+10000,source:'nats' as const,kind:'server.shutdown',shortLabel:'Shutdown',label:'Recorded shutdown',severity:'critical' as const};
const tl:Timeline={start,bucketMs:60000,bucketCount:2,rows:[],kubernetes:[],nats:[e],windows:[],natsTruncated:false,kubernetesTruncated:false,findingTruncated:false,eventReadId:'9007199254740995',evidenceRange:range};
it('opens recorded details inside the persistent view and keeps the event boundary',()=>{
 vi.mocked(useCapturedEvent).mockReturnValue(result({id:e.id,time:e.time,receivedAt:e.time+40,source:'nats',kind:e.kind,type:'SERVER.SHUTDOWN',summary:e.label,reason:'Operator requested shutdown',server:'nats-0',account:'',stream:'',consumer:'',clientName:'',clientHost:'',raised:[],raw:'{"reason":"generic"}'}));
 render(<MemoryRouter initialEntries={['/?system=shop&range=1h']}><StoryLanes tl={tl} end={end} live/></MemoryRouter>);
 fireEvent.click(screen.getByRole('button',{name:'NATS event: Recorded shutdown'}));
 expect(screen.getByRole('dialog')).toHaveTextContent('Operator requested shutdown');
 expect(useCapturedEvent).toHaveBeenLastCalledWith(e.eventId,'9007199254740995');
 const link=screen.getByRole('link',{name:'Open recorded event'}).getAttribute('href')!;
 const p=new URL(link,'http://example.com').searchParams;
 expect(p.get('eventReadId')).toBe('9007199254740995');expect(p.get('system')).toBe('shop');expect(p.has('range')).toBe(false);expect(Date.parse(p.get('from')!)).toBe(start);
});
it('groups overlapping ticks and lists every captured member instead of hiding one behind another',()=>{
 vi.mocked(useSelectedTimelineEvents).mockReturnValue(result({items:[],totalCount:0,nextPageToken:''}));
 render(<MemoryRouter initialEntries={['/?system=shop']}><StoryLanes tl={{...tl,nats:[e,{...e,id:'9007199254740994',eventId:'9007199254740994',label:'Second overlapping event'}]}} end={end} live={false}/></MemoryRouter>);
 fireEvent.click(screen.getByRole('button',{name:/2 overlapping Events/}));
 expect(screen.getByRole('dialog')).toHaveTextContent('Recorded shutdown');expect(screen.getByRole('dialog')).toHaveTextContent('Second overlapping event');
 expect(screen.getByRole('dialog')).toHaveTextContent(/2 captured/);
});
it('opens the actual inference explanation and exact window input identities',()=>{
 vi.mocked(useSelectedTimelineEvents).mockReturnValue(result({items:[],totalCount:0,nextPageToken:''}));
 render(<MemoryRouter initialEntries={['/?system=shop']}><StoryLanes tl={{...tl,windows:[{id:'a'.repeat(64),start,end,label:'Node drain',detail:'One drain',derivation:'Groups node and NATS pod observations within 15 minutes',supportingEventIds:[e.id],evidenceTotal:1,eventReadId:tl.eventReadId,captureRange:range}]}} end={end} live={false}/></MemoryRouter>);
 fireEvent.click(screen.getByRole('button',{name:'Window: Node drain'}));
 expect(screen.getByRole('dialog')).toHaveTextContent('Groups node and NATS pod observations within 15 minutes');
 expect(screen.getByRole('link',{name:'Open complete window evidence'})).toHaveAttribute('href',expect.stringContaining('a'.repeat(64)));
});
it('reads contributor pages only after an explicit action and keeps missing captured labels',async()=>{
 const {CapturedEventList}=await import('./TimelineEventEvidence');
 vi.mocked(useSelectedTimelineEvents).mockReturnValue(result({items:[],totalCount:0,nextPageToken:''}));
 const ids=Array.from({length:51},(_,i)=>`${i+1}`);
 render(<MemoryRouter><CapturedEventList ids={ids} range={range} readId="55" captured={[{...e,id:'1',eventId:'1',label:'Retained captured label'}]}/></MemoryRouter>);
 expect(screen.getByText('Retained captured label')).toBeInTheDocument();expect(screen.getAllByRole('button',{name:'Read event detail'})).toHaveLength(50);
 expect(useSelectedTimelineEvents).toHaveBeenLastCalledWith(range,ids.slice(0,50),'55');
 fireEvent.click(screen.getByRole('button',{name:'Next events'}));expect(screen.getAllByRole('button',{name:'Read event detail'})).toHaveLength(1);expect(useSelectedTimelineEvents).toHaveBeenLastCalledWith(range,['51'],'55');
});
it('opens the Findings lane in retained occurrence history with the captured range and transition boundary',()=>{
 render(<MemoryRouter initialEntries={['/?system=shop&range=1h']}><StoryLanes tl={{...tl,findingReadEpoch:'0'}} end={end} live/></MemoryRouter>);
 const link=screen.getByRole('link',{name:'Findings'}).getAttribute('href')!;const u=new URL(link,'https://example.com');expect(u.pathname).toBe('/findings/history');expect(u.searchParams.get('readEpoch')).toBe('0');expect(Date.parse(u.searchParams.get('from')!)).toBe(start);expect(u.searchParams.has('range')).toBe(false);
});
it('keeps finding and collector observations in the Events lane destination instead of hiding them as routine',()=>{
 render(<MemoryRouter initialEntries={['/?system=shop']}><StoryLanes tl={{...tl,nats:[e,{...e,id:'2',source:'finding'},{...e,id:'3',source:'collector'}]}} end={end} live={false}/></MemoryRouter>);
 const events=new URL(screen.getByRole('link',{name:'Events'}).getAttribute('href')!,'https://example.com');expect(events.searchParams.get('src')?.split(',')).toEqual(['nats','findings','collector']);expect(events.searchParams.get('show')).toBe('all');expect(events.searchParams.get('eventReadId')).toBe(tl.eventReadId);
 const k8s=new URL(screen.getByRole('link',{name:'Kubernetes'}).getAttribute('href')!,'https://example.com');expect(k8s.searchParams.get('show')).toBe('all');
});

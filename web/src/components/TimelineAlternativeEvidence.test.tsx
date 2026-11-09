import {fireEvent,render,screen,within} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {expect,it,vi} from 'vitest';
import type {Timeline} from '../api/model';
import {StoryLanes} from './StoryLanes';
import {StatusTimeline} from './StatusTimeline';
vi.mock('../api/timelineEvidence',()=>({useCapturedEvent:()=>({data:undefined,isPending:true,error:null}),useSelectedTimelineEvents:()=>({data:{items:[],totalCount:0},isPending:false,error:null})}));
vi.mock('../api/findingEpisodes',()=>({useFindingEpisodes:()=>({data:{items:[],totalCount:0,nextPageToken:'',readEpoch:'42'},isPending:false,error:null})}));
const start=Date.parse('2026-10-17T23:59:00Z');
const range={start:'2026-10-17T23:59:00.000123Z',end:'2026-10-18T00:01:00.000456Z'};
const tl:Timeline={start,bucketMs:60000,bucketCount:2,rows:[{key:'consumer-006:APP/orders/worker',checkCode:'consumer-006',checkName:'Delivery stalled',severity:'warning',cells:['warning','nodata'],now:'ok',entity:{kind:'consumer',key:'APP/orders/worker',name:'worker',account:'APP',stream:'orders',consumer:'worker',server:''}}],kubernetes:[{id:'k1',eventId:'91',time:start+90000,source:'kubernetes',kind:'k8s.pod.ready',shortLabel:'Ready',label:'nats-0 ready'}],nats:[],windows:[{id:'a'.repeat(64),start,end:start+90000,label:'Observed rollout',detail:'Grouped pod evidence',derivation:'Observed pod events within the grouping gap',captureRange:range,eventReadId:'99',supportingEventIds:['91']}],findingReadEpoch:'42',eventReadId:'99',evidenceRange:range,natsTruncated:false,findingTruncated:false,kubernetesTruncated:true};
function table(){render(<MemoryRouter initialEntries={['/?system=shop']}><StoryLanes tl={tl} end={start+120000} live={false}/></MemoryRouter>);fireEvent.click(screen.getByRole('button',{name:'Each check'}));fireEvent.click(screen.getByRole('button',{name:'Show as table'}));return screen.getByRole('table')}
it('opens the same typed fixed history from expanded chart and table check labels',()=>{
 const {unmount}=render(<MemoryRouter initialEntries={['/?system=shop']}><StatusTimeline tl={tl} end={start+120000} live={false} asTable={false}/></MemoryRouter>);
 let link=screen.getByRole('link',{name:'consumer-006 occurrence history'});const chart=link.getAttribute('href');unmount();const t=table();link=within(t).getByRole('link',{name:'consumer-006 occurrence history'});expect(link).toHaveAttribute('href',chart!);const u=new URL(chart!,'https://example.com');expect(u.pathname).toBe('/findings/history');expect(Object.fromEntries(u.searchParams)).toMatchObject({system:'shop',readEpoch:'42',check:'consumer-006',type:'consumer',key:'APP/orders/worker',from:range.start,to:range.end});expect(u.searchParams.has('range')).toBe(false);
});
it('makes table Kubernetes records inspectable and retains captured source limits',()=>{
 const t=table(),mark=within(t).getByRole('button',{name:'Kubernetes event: nats-0 ready'});fireEvent.focus(mark);expect(screen.getByRole('tooltip')).toHaveTextContent('2026-10-18 00:00:30 UTC');fireEvent.click(mark);const dialog=screen.getByRole('dialog');expect(dialog).toHaveTextContent('Kubernetes events');expect(within(dialog).getByRole('link',{name:'Open recorded event'})).toHaveAttribute('href',expect.stringContaining('eventReadId=99'));
});
it('keeps inferred windows inspectable in the table and dates explicit across midnight',()=>{
 const t=table();expect(t).toHaveTextContent('warning 2026-10-17 23:59:00 UTC');expect(t).toHaveTextContent('no data 2026-10-18 00:00:00 UTC');fireEvent.click(within(t).getByRole('button',{name:'Window: Observed rollout'}));expect(screen.getByRole('dialog')).toHaveTextContent('Observed pod events within the grouping gap');expect(within(screen.getByRole('dialog')).getByRole('link',{name:'Open complete window evidence'})).toHaveAttribute('href',expect.stringContaining('eventReadId=99'));
});
it('links the expanded Kubernetes label to captured events rather than an unrelated current feed',()=>{
 render(<MemoryRouter initialEntries={['/?system=shop']}><StatusTimeline tl={tl} end={start+120000} live={false} asTable={false}/></MemoryRouter>);const u=new URL(screen.getByRole('link',{name:'Kubernetes event history'}).getAttribute('href')!,'https://example.com');expect(Object.fromEntries(u.searchParams)).toMatchObject({system:'shop',from:range.start,to:range.end,eventReadId:'99',src:'kubernetes',show:'all'});
});
it('uses the visible fixed interval when event-only Timelines have no check buckets',()=>{
 render(<MemoryRouter initialEntries={['/?system=shop']}><StatusTimeline tl={{...tl,bucketCount:0,bucketMs:0,rows:[],windows:[],evidenceRange:undefined}} end={start+120000} live={false} asTable/></MemoryRouter>);
 fireEvent.click(screen.getByRole('button',{name:'Kubernetes event: nats-0 ready'}));const u=new URL(screen.getByRole('link',{name:'Open recorded event'}).getAttribute('href')!,'https://example.com');expect(Date.parse(u.searchParams.get('from')!)).toBe(start);expect(Date.parse(u.searchParams.get('to')!)).toBe(start+120000);
});

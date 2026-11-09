import {fireEvent,render,screen,within} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {beforeEach,expect,it,vi} from 'vitest';
import type {UseQueryResult} from '@tanstack/react-query';
import type {Timeline} from '../api/model';
import {StoryLanes} from './StoryLanes';
import {StatusTimeline} from './StatusTimeline';
import {ZoomStrip} from './ZoomStrip';
import {useCapturedEvent,useSelectedTimelineEvents} from '../api/timelineEvidence';
const measure=vi.hoisted(()=>({width:800}));
vi.mock('../lib/useWidth',()=>({useWidth:()=>measure.width}));
vi.mock('../api/timelineEvidence',()=>({useCapturedEvent:vi.fn(),useSelectedTimelineEvents:vi.fn()}));
vi.mock('./InvestigationContext',()=>({InvestigationButton:({selection}:{selection:{annotation?:string}})=><span data-testid="prompt-annotation">{selection.annotation}</span>}));
const result=<T,>(data:T)=>({data,isPending:false,error:null} as UseQueryResult<T,Error>);
const start=Date.parse('2026-10-17T12:00:00Z'),end=start+120000;
const event={id:'1',eventId:'1',time:start+10000,source:'nats' as const,kind:'server.shutdown',shortLabel:'Shutdown',label:'Recorded shutdown'};
const second={...event,id:'2',eventId:'2',label:'Second observation'};
const tl:Timeline={start,bucketMs:60000,bucketCount:2,rows:[],kubernetes:[],nats:[event,second],windows:[],natsTruncated:false,findingTruncated:false,kubernetesTruncated:false,eventReadId:'2'};
const win={start,end,label:'Observed window',detail:'Observed inputs'};
beforeEach(()=>{measure.width=800;vi.mocked(useCapturedEvent).mockReturnValue(result(undefined));vi.mocked(useSelectedTimelineEvents).mockReturnValue(result({items:[],totalCount:0,nextPageToken:''}))});
for(const [flag,label] of [['natsTruncated','NATS and collector events'],['findingTruncated','finding transitions']] as const){
 it(`keeps ${flag} in group previews and the selected snapshot after refresh`,()=>{
 const selected={...tl,[flag]:true};const {rerender}=render(<MemoryRouter><StoryLanes tl={selected} end={end} live/></MemoryRouter>);
 const mark=screen.getByRole('button',{name:'2 overlapping Events marks'});fireEvent.focus(mark);expect(screen.getByRole('tooltip')).toHaveTextContent('Source Timeline input limit reached');expect(screen.getByRole('tooltip')).toHaveTextContent(label);
 fireEvent.click(mark);expect(within(screen.getByRole('dialog')).getByRole('note')).toHaveTextContent('Source Timeline input limit reached');
 rerender(<MemoryRouter><StoryLanes tl={{...tl,nats:[]}} end={end} live/></MemoryRouter>);expect(screen.getByRole('dialog')).toHaveTextContent(label);
 });
}
it('carries the Kubernetes limit into expanded per-check event details',()=>{
 const k8s={...event,source:'kubernetes' as const};render(<MemoryRouter><StatusTimeline tl={{...tl,nats:[],kubernetes:[k8s,{...k8s,id:'2',eventId:'2'}],kubernetesTruncated:true}} end={end} live={false} asTable={false}/></MemoryRouter>);
 fireEvent.click(screen.getByRole('button',{name:'2 overlapping Kubernetes marks'}));expect(screen.getByRole('dialog')).toHaveTextContent('Kubernetes events');expect(screen.getByRole('dialog')).toHaveTextContent('Source Timeline input limit reached');
});
it.each([360,800])('keeps source limits in a %ipx zoom without claiming they were omitted inside that window',width=>{measure.width=width;
 render(<MemoryRouter><ZoomStrip tl={{...tl,findingTruncated:true}} win={win}/></MemoryRouter>);fireEvent.click(screen.getByRole('button',{name:'2 overlapping observations'}));expect(screen.getByRole('dialog')).toHaveTextContent('Source Timeline input limit reached');expect(screen.getByRole('dialog')).toHaveTextContent('This view may omit other observations');
});
it('keeps limits on single events and includes them in the copied evidence annotation',()=>{
 vi.mocked(useCapturedEvent).mockReturnValue(result({id:'1',time:event.time,source:'nats',kind:event.kind,type:'SERVER.SHUTDOWN',summary:event.label,reason:'',server:'',account:'',stream:'',consumer:'',clientName:'',clientHost:'',raised:[],raw:''}));
 render(<MemoryRouter><StoryLanes tl={{...tl,nats:[event],natsTruncated:true}} end={end} live={false}/></MemoryRouter>);fireEvent.click(screen.getByRole('button',{name:'NATS event: Recorded shutdown'}));expect(within(screen.getByRole('dialog')).getByRole('note')).toHaveTextContent('Source Timeline input limit reached');expect(screen.getByTestId('prompt-annotation')).toHaveTextContent('NATS and collector events');
});
it('does not assign an unrelated lane limit or declare complete capture when flags are false',()=>{
 render(<MemoryRouter><StoryLanes tl={{...tl,kubernetesTruncated:true}} end={end} live={false}/></MemoryRouter>);fireEvent.click(screen.getByRole('button',{name:'2 overlapping Events marks'}));expect(screen.getByRole('dialog')).not.toHaveTextContent('Source Timeline input limit reached');expect(screen.getByRole('dialog')).not.toHaveTextContent('capture is complete');
});

it('does not turn a lane limit into an inferred-window input limit',()=>{
 render(<MemoryRouter><StoryLanes tl={{...tl,windows:[{...win,supportingEventIds:['1']}],kubernetesTruncated:true}} end={end} live={false}/></MemoryRouter>);fireEvent.click(screen.getByRole('button',{name:'Window: Observed window'}));expect(screen.getByRole('dialog')).not.toHaveTextContent('Source Timeline input limit reached');
});

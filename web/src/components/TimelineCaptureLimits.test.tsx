import {fireEvent,render,screen} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {beforeEach,expect,it,vi} from 'vitest';
import type {UseQueryResult} from '@tanstack/react-query';
import type {Timeline} from '../api/model';
import {StoryLanes} from './StoryLanes';
import {StatusTimeline} from './StatusTimeline';
import {ZoomStrip} from './ZoomStrip';
import {useCapturedEvent,useSelectedTimelineEvents} from '../api/timelineEvidence';
vi.mock('../lib/useWidth',()=>({useWidth:()=>800}));
vi.mock('../api/timelineEvidence',()=>({useCapturedEvent:vi.fn(),useSelectedTimelineEvents:vi.fn()}));
vi.mock('./InvestigationContext',()=>({InvestigationButton:({selection}:{selection:{annotation?:string}})=><span data-testid="prompt-annotation">{selection.annotation}</span>}));
const result=<T,>(data:T)=>({data,isPending:false,error:null} as UseQueryResult<T,Error>);
const start=Date.parse('2026-10-17T12:00:00Z'),end=start+120000;
const event={id:'1',eventId:'1',time:start+10000,source:'nats' as const,kind:'server.shutdown',shortLabel:'Shutdown',label:'Recorded shutdown'};
const second={...event,id:'2',eventId:'2',label:'Second observation'};
const tl:Timeline={start,bucketMs:60000,bucketCount:2,rows:[],kubernetes:[],nats:[event,second],windows:[],natsTruncated:false,findingTruncated:false,kubernetesTruncated:false,eventReadId:'2'};
const win={start,end,label:'Observed window'};
beforeEach(()=>{vi.mocked(useCapturedEvent).mockReturnValue(result(undefined));vi.mocked(useSelectedTimelineEvents).mockReturnValue(result({items:[],totalCount:0,nextPageToken:''}))});
for(const [flag,label] of [['natsTruncated','NATS and collector events'],['findingTruncated','finding transitions']] as const){
 it(`keeps ${flag} in group previews and the selected snapshot after refresh`,()=>{
 const selected={...tl,[flag]:true};const {rerender}=render(<MemoryRouter><StoryLanes tl={selected} end={end} live/></MemoryRouter>);
 const mark=screen.getByRole('button',{name:'2 overlapping Events marks'});fireEvent.focus(mark);expect(screen.getByRole('tooltip')).toHaveTextContent('Source Timeline capture was truncated');expect(screen.getByRole('tooltip')).toHaveTextContent(label);
 fireEvent.click(mark);expect(screen.getByRole('dialog')).toHaveTextContent('Source Timeline capture was truncated');
 rerender(<MemoryRouter><StoryLanes tl={{...tl,nats:[]}} end={end} live/></MemoryRouter>);expect(screen.getByRole('dialog')).toHaveTextContent(label);
 });
}
it('carries the Kubernetes limit into expanded per-check event details',()=>{
 const k8s={...event,source:'kubernetes' as const};render(<MemoryRouter><StatusTimeline tl={{...tl,nats:[],kubernetes:[k8s,{...k8s,id:'2',eventId:'2'}],kubernetesTruncated:true}} end={end} live={false} asTable={false}/></MemoryRouter>);
 fireEvent.click(screen.getByRole('button',{name:'2 overlapping Kubernetes marks'}));expect(screen.getByRole('dialog')).toHaveTextContent('Kubernetes events');expect(screen.getByRole('dialog')).toHaveTextContent('Source Timeline capture was truncated');
});
it('keeps source limits on zoom contributors without claiming they were omitted inside that window',()=>{
 render(<MemoryRouter><ZoomStrip tl={{...tl,findingTruncated:true}} win={win}/></MemoryRouter>);fireEvent.click(screen.getByRole('button',{name:'2 overlapping observations'}));expect(screen.getByRole('dialog')).toHaveTextContent('Source Timeline capture was truncated');expect(screen.getByRole('dialog')).toHaveTextContent('This view may omit other observations');
});
it('keeps limits on single events and includes them in the copied evidence annotation',()=>{
 vi.mocked(useCapturedEvent).mockReturnValue(result({id:'1',time:event.time,source:'nats',kind:event.kind,type:'SERVER.SHUTDOWN',summary:event.label,server:'',account:'',stream:'',consumer:'',clientName:'',clientHost:'',raised:[],raw:''}));
 render(<MemoryRouter><StoryLanes tl={{...tl,nats:[event],natsTruncated:true}} end={end} live={false}/></MemoryRouter>);fireEvent.click(screen.getByRole('button',{name:'NATS event: Recorded shutdown'}));expect(screen.getByRole('dialog')).toHaveTextContent('Source Timeline capture was truncated');expect(screen.getByTestId('prompt-annotation')).toHaveTextContent('NATS and collector events');
});
it('does not assign an unrelated lane limit or declare complete capture when flags are false',()=>{
 render(<MemoryRouter><StoryLanes tl={{...tl,kubernetesTruncated:true}} end={end} live={false}/></MemoryRouter>);fireEvent.click(screen.getByRole('button',{name:'2 overlapping Events marks'}));expect(screen.getByRole('dialog')).not.toHaveTextContent('Source Timeline capture was truncated');expect(screen.getByRole('dialog')).not.toHaveTextContent('capture is complete');
});

import {fireEvent,render,screen,within} from '@testing-library/react';
import {MemoryRouter} from 'react-router';
import {beforeEach,expect,it,vi} from 'vitest';
import type {UseQueryResult} from '@tanstack/react-query';
import type {LaneEvent,Timeline} from '../api/model';
import {StoryLanes} from './StoryLanes';
import {useCapturedEvent,useSelectedTimelineEvents} from '../api/timelineEvidence';
const measure=vi.hoisted(()=>({width:800}));
vi.mock('../lib/useWidth',()=>({useWidth:()=>measure.width}));
vi.mock('../lib/useSystem',()=>({useSystem:()=>'example-prod'}));
vi.mock('../api/timelineEvidence',()=>({useCapturedEvent:vi.fn(),useSelectedTimelineEvents:vi.fn()}));
vi.mock('./InvestigationContext',()=>({InvestigationButton:({selection}:{selection:unknown})=><span data-testid="prompt-selection">{JSON.stringify(selection)}</span>}));
const result=<T,>(data:T)=>({data,isPending:false,error:null} as UseQueryResult<T,Error>);
const start=Date.parse('2026-10-17T09:00:00Z'),end=start+60*60_000;
const restart:LaneEvent={id:'restart:N1',time:start+18*60_000,source:'nats',kind:'server.restarted',shortLabel:'nats-0 restart',label:'nats-0 restarted, version 2.12.2 unchanged',entity:{kind:'server',key:'nats-0',name:'nats-0',account:'',stream:'',consumer:'',server:'nats-0'}};
const lameduck:LaneEvent={id:'7',eventId:'7',time:start+40*60_000,source:'nats',kind:'server.lameduck',shortLabel:'SERVER.LAMEDUCK',label:'SERVER.LAMEDUCK nats-1 entered lame duck mode'};
const elect=(id:string,kind:string,t:number):LaneEvent=>({id,eventId:id,time:start+t,source:'nats',kind,shortLabel:kind,label:`${kind} ${id}`});
const tl:Timeline={start,bucketMs:5*60_000,bucketCount:12,rows:[],kubernetes:[],nats:[],windows:[],natsTruncated:false,findingTruncated:false,kubernetesTruncated:false,eventReadId:'99',
 servers:[restart,lameduck],serversTruncated:false,leaders:[elect('11','stream.leader_elected',16*60_000),elect('12','consumer.leader_elected',16*60_000+1000),elect('13','meta.leader_elected',30*60_000)],leadersTruncated:false,
 clients:[{index:0,connects:4,closes:4,drops:0,shutdowns:0,authErrors:0,unknownDisconnects:0},{index:3,connects:6,closes:1,drops:2,shutdowns:3,authErrors:1,unknownDisconnects:0}],systemEventsCaptured:true,jetstreamEventsCaptured:true};
const draw=(t:Timeline=tl)=>render(<MemoryRouter><StoryLanes tl={t} end={end} live={false}/></MemoryRouter>);
beforeEach(()=>{measure.width=800;vi.mocked(useCapturedEvent).mockReturnValue(result(undefined));vi.mocked(useSelectedTimelineEvents).mockReturnValue(result({items:[],totalCount:0,nextPageToken:''}))});

it('labels each new lane with its count and links it to the captured Events view of its kinds',()=>{
 draw();
 const link=(name:string)=>new URL(screen.getByRole('link',{name}).getAttribute('href')!,'http://x');
 for(const [name,kinds] of [['Servers','server.lameduck,server.shutdown,server.out_of_storage,server.removed'],['Leaders','meta.leader_elected,stream.leader_elected,consumer.leader_elected'],['Clients','client.connect,client.disconnect,client.churn,client.auth_error']]){
  const u=link(name);expect(u.pathname).toBe('/events');expect(u.searchParams.get('kind')).toBe(kinds);expect(u.searchParams.get('eventReadId')).toBe('99');expect(u.searchParams.get('from')).toBe('2026-10-17T09:00:00.000Z');
 }
 expect(screen.getByRole('group',{name:'Servers: 1 restart, 1 server event'})).toBeInTheDocument();
 expect(screen.getByRole('group',{name:'Leader elections: 3, 1 meta'})).toBeInTheDocument();
 expect(screen.getByRole('group',{name:'Client connections: 10 connects, 5 client closes, 2 drops, 3 server-shutdown disconnects, 1 failed authentication'})).toBeInTheDocument();
 expect(screen.getByText('1 restart, 1 event')).toBeInTheDocument();
});

it('previews a restart with its UTC time and opens it as derived from the collections',()=>{
 draw();
 const mark=screen.getByRole('button',{name:'Server restart: nats-0 restarted, version 2.12.2 unchanged'});
 fireEvent.focus(mark);expect(screen.getByRole('tooltip')).toHaveTextContent('2026-10-17 09:18:00 UTC');
 fireEvent.click(mark);const dialog=screen.getByRole('dialog');
 expect(dialog).toHaveTextContent('Derived from the collections');expect(dialog).toHaveTextContent('no recorded-event ID');
 expect(within(dialog).getByRole('link',{name:'Server nats-0'}).getAttribute('href')).toMatch(/^\/servers\/nats-0\?system=example-prod&from=/);
});

it('groups overlapping leader elections with bounded membership and keeps the leader limit',()=>{
 draw({...tl,leadersTruncated:true});
 const group=screen.getByRole('button',{name:'2 overlapping Leaders marks'});
 fireEvent.focus(group);const tip=screen.getByRole('tooltip');
 expect(tip).toHaveTextContent('Visual overlap, not one incident');expect(tip).toHaveTextContent('Source Timeline input limit reached: leader elections');
 fireEvent.click(group);expect(screen.getByRole('dialog')).toHaveTextContent('2 captured event identities');
 // The single meta election is its own mark.
 expect(screen.getByRole('button',{name:'Leader election: meta.leader_elected 13'})).toHaveClass('meta');
});

it('draws client counts as bucket marks, drops highlighted, never as one event',()=>{
 draw();
 const quiet=screen.getByRole('button',{name:'Client connections 09:00–09:05: 4 connects, 4 client closes'});
 const busy=screen.getByRole('button',{name:'Client connections 09:15–09:20: 6 connects, 1 client close, 2 drops, 3 server-shutdown disconnects, 1 failed authentication'});
 expect(quiet).not.toHaveClass('drops');expect(busy).toHaveClass('drops');
 fireEvent.pointerEnter(busy);const tip=screen.getByRole('tooltip');
 expect(tip).toHaveTextContent('2026-10-17 09:15:00 UTC – 2026-10-17 09:20:00 UTC');expect(tip).toHaveTextContent('not one event');
 expect(tip).toHaveTextContent('Disconnected by a server shutdown: 3');
 fireEvent.click(busy);const dialog=screen.getByRole('dialog');
 const all=new URL(within(dialog).getByRole('link',{name:'Open these client events'}).getAttribute('href')!,'http://x');
 expect(all.searchParams.get('from')).toBe('2026-10-17T09:15:00.000Z');expect(all.searchParams.get('to')).toBe('2026-10-17T09:20:00.000Z');
 expect(all.searchParams.get('kind')).toBe('client.connect,client.disconnect,client.churn,client.auth_error');expect(all.searchParams.get('eventReadId')).toBe('99');
 expect(new URL(within(dialog).getByRole('link',{name:'Open the disconnects'}).getAttribute('href')!,'http://x').searchParams.get('kind')).toBe('client.disconnect');
 expect(within(dialog).getByRole('link',{name:'Open the failed authentications'})).toBeInTheDocument();
});

it('merges client buckets into wider marks on a narrow track',()=>{
 measure.width=30;draw();
 expect(screen.getByRole('button',{name:'Client connections 09:00–09:25: 10 connects, 5 client closes, 2 drops, 3 server-shutdown disconnects, 1 failed authentication'})).toBeInTheDocument();
});

it('says when the sources were not captured instead of drawing a quiet lane',()=>{
 draw({...tl,leaders:[],clients:[],servers:[restart],systemEventsCaptured:false,jetstreamEventsCaptured:false});
 expect(screen.getByText('JetStream advisories not captured')).toBeInTheDocument();
 expect(screen.getAllByText('System events not captured')).toHaveLength(2);
 expect(screen.getByRole('group',{name:'Client connections: system events not captured'})).toBeInTheDocument();
 // Restarts come from the collections and stay.
 expect(screen.getByRole('button',{name:/Server restart/})).toBeInTheDocument();
});

it('says none in range when the sources were captured but the range is quiet',()=>{
 draw({...tl,leaders:[],clients:[],servers:[]});
 expect(screen.getAllByText('none in range')).toHaveLength(3);
 expect(screen.queryByText(/not captured/)).not.toBeInTheDocument();
});

it('draws no subject lanes for a source that does not report them',()=>{
 const {servers:_s,leaders:_l,clients:_c,...old}=tl;
 draw(old);
 expect(screen.queryByRole('link',{name:'Servers'})).not.toBeInTheDocument();
 expect(screen.queryByRole('link',{name:'Clients'})).not.toBeInTheDocument();
});

it('explains restarts inside an overlap group as derived from the collections',()=>{
 const second={...restart,id:'restart:N2',time:restart.time+1000,label:'nats-1 restarted, version 2.12.2 unchanged',entity:{...restart.entity!,key:'nats-1',name:'nats-1',server:'nats-1'}};
 draw({...tl,servers:[restart,second]});
 fireEvent.click(screen.getByRole('button',{name:'2 overlapping Servers marks'}));
 const dialog=screen.getByRole('dialog');
 expect(within(dialog).getAllByText(/Derived from the collections/)).toHaveLength(2);
 expect(within(dialog).getByRole('link',{name:'Server nats-1'})).toBeInTheDocument();
});

it('counts closes and shutdown disconnects: a lane with only those is not "none in range"',()=>{
 draw({...tl,clients:[{index:2,connects:0,closes:40,drops:0,shutdowns:25,authErrors:0,unknownDisconnects:0}]});
 expect(screen.getAllByRole('button',{name:/^Client connections 09:10/})).toHaveLength(1);
 const note=screen.getByRole('link',{name:'Clients'}).parentElement!.querySelector('small')!.textContent;
 expect(note).toBe('40 client closes, 25 server-shutdown disconnects');
 expect(screen.getByRole('group',{name:'Client connections: 40 client closes, 25 server-shutdown disconnects'})).toBeInTheDocument();
});

it('counts every member of a group of restarts, not only recorded identities',()=>{
 const second={...restart,id:'restart:N2',time:restart.time+1000,label:'nats-1 restarted, version 2.12.2 unchanged',entity:{...restart.entity!,key:'nats-1',name:'nats-1',server:'nats-1'}};
 draw({...tl,servers:[restart,second,{...lameduck,time:restart.time+2000}]});
 fireEvent.click(screen.getByRole('button',{name:'3 overlapping Servers marks'}));
 const dialog=screen.getByRole('dialog');
 expect(dialog).toHaveTextContent('3 observations: 1 recorded event, 2 restarts derived from the collections.');
 expect(dialog).not.toHaveTextContent('0 captured event identities');
});

it('hatches the spans where event capture stopped and says so',()=>{
 draw({...tl,eventCaptureGaps:[{start:start+30*60_000,end:start+45*60_000}]});
 const marks=screen.getAllByRole('button',{name:'Event capture stopped 09:30–09:45'});
 expect(marks).toHaveLength(3);
 fireEvent.focus(marks[0]);
 expect(screen.getByRole('tooltip')).toHaveTextContent('2026-10-17 09:30:00 UTC – 2026-10-17 09:45:00 UTC');
 expect(screen.getByRole('tooltip')).toHaveTextContent('not because it was quiet');
 expect(screen.getByRole('group',{name:/^Client connections: .*; capture stopped 09:30–09:45$/})).toBeInTheDocument();
});

it('says capture stopped for the whole range instead of none in range',()=>{
 draw({...tl,servers:[],leaders:[],clients:[],systemEventsCaptured:false,jetstreamEventsCaptured:false,eventCaptureGaps:[{start,end}]});
 expect(screen.getAllByText('Event capture stopped for the whole range')).toHaveLength(3);
 expect(screen.queryByText('none in range')).not.toBeInTheDocument();
 expect(screen.getByRole('group',{name:'Client connections: event capture stopped for the whole range'})).toBeInTheDocument();
});

it('gives client cells a bound events query and restarts a server selection for the agent prompt',()=>{
 draw();
 fireEvent.click(screen.getByRole('button',{name:/^Client connections 09:15–09:20/}));
 const cell=JSON.parse(within(screen.getByRole('dialog')).getByTestId('prompt-selection').textContent!);
 expect(cell.query).toEqual({method:'ListEvents',request:{range:{start:'2026-10-17T09:15:00.000Z',end:'2026-10-17T09:20:00.000Z'},kinds:['client.connect','client.disconnect','client.churn','client.auth_error'],pageSize:50,eventReadId:'99'}});
 fireEvent.keyDown(document,{key:'Escape'});
 fireEvent.click(screen.getByRole('button',{name:/^Server restart/}));
 const sel=JSON.parse(within(screen.getByRole('dialog')).getByTestId('prompt-selection').textContent!);
 expect(sel.entity).toMatchObject({kind:'server',key:'nats-0'});expect(sel.annotation).toContain('derived from the collections');
});

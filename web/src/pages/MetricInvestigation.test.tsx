import {fireEvent,screen,within} from '@testing-library/react';
import {beforeEach,expect,it,vi} from 'vitest';
import {Route,Routes} from 'react-router';
import {MetricInvestigation} from './MetricInvestigation';
import {renderApp} from '../test/render';
import type {MetricComparison} from '../api/metricComparison';
import type {TrendSeries} from '../api/overview';
vi.mock('../api/systems',()=>({useSystems:()=>({data:undefined})}));
const start=Date.parse('2026-10-17T13:00:00Z'),end=start+3600000;
const history:TrendSeries={metric:'server.cpu_percent',unit:'percent',entity:{kind:'server',key:'nats-0',name:'nats-0',server:'nats-0',account:'',stream:'',consumer:''},stepMs:30000,points:[{time:start,value:46}],min:46,max:46,topPoints:[],observations:{sampleCount:4n,knownCount:3n,firstSampleAt:start,lastSampleAt:end-30000,maximum:100,mean:46.7,maximumAt:start+30000}};
let value:MetricComparison;
const refetch=vi.fn();vi.mock('../api/metricComparison',()=>({useMetricComparison:()=>({data:value,isPending:false,isFetching:false,error:null,refetch}),METRIC_PAGE_SIZE:12}));
beforeEach(()=>{value={system:'example-prod',start,end,readAt:end,rows:[{name:'nats-0',series:[history],errors:[]}],fleet:[{...history,metric:'cluster.cpu_percent'}],fleetErrors:[],total:1,page:0,pages:1} as MetricComparison});
function mount(url='/metrics/cpu?system=example-prod&account=APP&range=1h'){return renderApp(<Routes><Route path="/metrics/:family" element={<MetricInvestigation/>}/></Routes>,url)}
it('shows observed peaks rather than bucket means, unknown latest values and precise evidence bounds',()=>{
 mount();expect(screen.getByRole('heading',{name:'CPU investigation'})).toBeInTheDocument();const table=screen.getByRole('table',{name:'Server observations'});expect(within(table).getByText('100%')).toBeInTheDocument();expect(within(table).getAllByText('Unknown').length).toBeGreaterThan(0);expect(screen.getByText(/percent of one core/i)).toBeInTheDocument();const events=screen.getByRole('link',{name:'Events for nats-0'});const u=new URL(events.getAttribute('href')!,'http://example.com');expect(u.searchParams.get('from')).toBe(new Date(start).toISOString().replace('.000Z','Z'));expect(u.searchParams.get('range')).toBeNull();expect(u.searchParams.get('server')).toBe('nats-0');expect(u.searchParams.get('account')).toBe('APP');
});
it('navigates from the effective last page and preserves entity/system context in comparisons',()=>{
 value={...value,page:2,pages:3,total:25};mount('/metrics/cpu?system=example-prod&account=APP&range=1h&page=99&server=nats-0');fireEvent.click(screen.getByRole('button',{name:'Previous servers'}));expect(screen.getByTestId('location')).toHaveTextContent('page=1');const compare=screen.getByRole('link',{name:'Compare all server names'});expect(compare.getAttribute('href')).not.toContain('server=');expect(compare.getAttribute('href')).toContain('system=example-prod');
});
it('covers memory, connection counts and both traffic directions without asserting volumes',()=>{
 mount('/metrics/messages?system=example-prod&range=1h');expect(screen.getByRole('heading',{name:'Message traffic investigation'})).toBeInTheDocument();expect(screen.getByText(/counter volumes are not supplied/i)).toBeInTheDocument();expect(screen.getByRole('columnheader',{name:'In observed peak'})).toBeInTheDocument();expect(screen.getByRole('columnheader',{name:'Out observed peak'})).toBeInTheDocument();
});
it('shows separate observed counter increases for both traffic directions without asserting complete volume',()=>{
 const withCounter=(metric:string,total:string)=>({...history,metric,observations:{...history.observations!,counterComparisons:{observedIncrease:total,comparisonCount:2n,serverIdCount:1n,excludedReports:1n,boundaryReports:0n}}});value={...value,rows:[{...value.rows[0],series:[withCounter('server.in_msgs_rate','123'),withCounter('server.out_msgs_rate','456')]}]};mount('/metrics/messages?system=example-prod&range=1h');const table=screen.getByRole('table',{name:'Server observations'});expect(within(table).getByText('123')).toBeInTheDocument();expect(within(table).getByText('456')).toBeInTheDocument();expect(screen.getByRole('columnheader',{name:'In comparable increase (messages)'})).toBeInTheDocument();expect(screen.getByText(/not a complete traffic volume/i)).toBeInTheDocument();
});

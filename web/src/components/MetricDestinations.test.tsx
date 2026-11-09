import {screen} from '@testing-library/react';
import {expect,it,vi} from 'vitest';
import {MetricBox} from './MetricBox';
import {renderApp} from '../test/render';
vi.mock('../api/systems',()=>({useSystems:()=>({data:undefined})}));
vi.mock('./MetricHelp',()=>({MetricHelp:()=>null}));
it('links a loading server metric to the same name and preserves global scope',()=>{
 renderApp(<MetricBox title="CPU" metricIds={['server.cpu_percent']} metricEntity={{kind:'server',key:'nats / east',name:'nats / east',server:'nats / east',account:'',stream:'',consumer:''}} fmt={String} start={0} end={1000} loading/>, '/servers?system=example-prod&account=APP&range=6h');
 const url=new URL(screen.getByRole('link',{name:'CPU'}).getAttribute('href')!,'http://example.com');expect(url.pathname).toBe('/metrics/cpu');expect(url.searchParams.get('server')).toBe('nats / east');expect(url.searchParams.get('system')).toBe('example-prod');expect(url.searchParams.get('account')).toBe('APP');
});

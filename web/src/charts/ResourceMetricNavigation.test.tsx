import {screen} from '@testing-library/react';
import {expect,it,vi} from 'vitest';
import {renderApp} from '../test/render';
import {SeriesChart} from './SeriesChart';
vi.mock('../api/systems',()=>({useSystems:()=>({data:undefined})}));
vi.mock('../components/MetricHelp',()=>({MetricHelp:()=>null}));
it('links resource chart titles without conflating bucket names and backing stream identities',()=>{renderApp(<SeriesChart title="Bytes" start={0} end={1000} series={{metric:'stream.bytes',unit:'bytes',points:[],entity:{kind:'stream',key:'APP/KV_inventory',account:'APP',stream:'KV_inventory',consumer:'',server:'',name:'KV_inventory'}}}/>, '/kv-stores/APP/inventory?system=example-prod&range=1h');const u=new URL(screen.getByRole('link',{name:'Bytes'}).getAttribute('href')!,'http://example.com');expect(u.pathname).toBe('/metrics/stream-bytes');expect(u.searchParams.get('stream')).toBe('APP/KV_inventory');expect(u.searchParams.get('system')).toBe('example-prod')});

import {screen} from '@testing-library/react';
import {expect,it,vi} from 'vitest';
import {OverviewTiles} from './OverviewTiles';
import type {Tile} from '../api/model';
import {renderApp} from '../test/render';
vi.mock('../api/systems',()=>({useSystems:()=>({data:undefined})}));
it.each([['servers','Servers','/metrics/servers'],['meta_leader','Meta leader','/metrics/meta-changes'],['streams','Streams','/metrics/streams'],['kv','KV buckets','/metrics/kv-buckets'],['object_stores','Object stores','/metrics/object-stores'],['consumers','Consumers','/metrics/durable-consumers'],['accounts','Accounts','/metrics/accounts'],['clients','Connections','/metrics/connections'],['storage','Storage','/capacity/storage-change']])('opens %s observations even without loaded tile history',(id,label,path)=>{const t:Tile={id,label,unit:'count',status:'ok',value:1,valueSuffix:'',value2Suffix:'',subtitle:'',description:'Captured metric'};renderApp(<OverviewTiles tiles={[t]} windows={[]} start={1000} end={2000}/>, '/?system=example-prod&range=1h');const u=new URL(screen.getByRole('link',{name:label}).getAttribute('href')!,'http://example.com');expect(u.pathname).toBe(path);expect(u.searchParams.get('system')).toBe('example-prod')});

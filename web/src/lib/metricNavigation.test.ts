import {expect,it} from 'vitest';
import {metricDestination,metricFamilies} from './metricNavigation';
const entity={kind:'server' as const,key:'nats / east',server:'nats / east',name:'nats / east',account:'',stream:'',consumer:''};
it.each([['cpu_percent','cpu'],['mem_bytes','memory'],['connections','connections'],['in_msgs_rate','messages'],['out_msgs_rate','messages'],['in_bytes_rate','bytes'],['out_bytes_rate','bytes']])('uses a canonical fleet and entity destination for %s',(metric,path)=>{
 expect(metricDestination({metric:`cluster.${metric}`})).toBe(`/metrics/${path}`);expect(metricDestination({metric:`server.${metric}`,entity})).toBe(`/metrics/${path}?server=nats+%2F+east`);
});
it('reuses specialist evidence routes and never guesses an unrelated metric destination',()=>{
 expect(metricDestination({metric:'server.slow_consumers',entity})).toBe('/servers/slow-consumers?server=nats+%2F+east');expect(metricDestination({metric:'cluster.js_storage_bytes'})).toBe('/capacity/storage-change');expect(metricDestination({metric:'unknown_metric'})).toBeUndefined();expect(metricFamilies.map(f=>f.key)).toEqual(['cpu','memory','connections','messages','bytes']);
});

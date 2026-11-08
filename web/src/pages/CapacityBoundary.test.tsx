import { expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { CapacityHistoryChart } from '../components/CapacityHistory';
import { OneLine } from '../components/Trends';
import { Sparkline } from '../charts/Sparkline';
import { renderApp } from '../test/render';
import type { Point } from '../api/model';
const base = Date.parse('2026-10-17T12:00:00Z');
const stepMs = 30000;
function series(points: Point[], metric = 'stream.bytes') {
 return {isPending:false,error:undefined,data:{metric,unit:'bytes' as const,stepMs,points,topPoints:[]}};
}
function route(from: number, to: number) {
 return `/capacity?from=${new Date(base+from).toISOString()}&to=${new Date(base+to).toISOString()}`;
}
for (const compact of [false, true]) {
 for (const metric of ['cluster.capacity_file_store_bytes', 'server.capacity_file_store_bytes', 'stream.bytes', 'stream.observed_replica_bytes']) {
  it(`places ${metric} ${compact?'compact':'expanded'} bucket overlap at its display midpoint while retaining its label`, () => {
   const v = renderApp(<CapacityHistoryChart q={series([{time:base,value:40}],metric)} title={metric} compact={compact}/>,route(15000,25000));
   expect(Number(v.container.querySelector('.tc-dot,.spark-dot')!.getAttribute('cx'))).toBe(compact?100:150);
   const slider=screen.getByRole('slider');
   for (const key of ['Home','End']) {
    fireEvent.keyDown(slider,{key});
    expect(screen.getByRole('tooltip')).toHaveTextContent('12:00:00 UTC');
    expect(screen.getByRole('tooltip')).toHaveTextContent('Bucket to 12:00:25 UTC');
    expect(screen.getByRole('tooltip')).toHaveTextContent('40 B');
    expect(screen.getByRole('tooltip')).toHaveTextContent('not sample times');
    expect(screen.getByRole('tooltip')).not.toHaveTextContent('12:00:20');
   }
   Object.defineProperty(slider,'getBoundingClientRect',{value:()=>({left:0,width:300})});
   fireEvent.pointerMove(slider,{clientX:150});
   expect(screen.getByRole('tooltip')).toHaveTextContent('40 B');
  });
 }
 it(`keeps ${compact?'compact':'expanded'} isolated first and last partial buckets and a genuine missing middle`,()=>{
  const v=renderApp(<CapacityHistoryChart q={series([{time:base,value:0},{time:base+60000,value:20}])} title="Bytes" compact={compact}/>,route(15000,65000));
  const dots=[...v.container.querySelectorAll('.tc-dot,.spark-dot')];
  expect(dots).toHaveLength(2);
  const width=compact?200:300;
  expect(dots.map(p=>Number(p.getAttribute('cx')))).toEqual([width*0.15,width*0.95]);
  expect(v.container.querySelectorAll('polyline')).toHaveLength(0);
  const slider=screen.getByRole('slider');
  fireEvent.keyDown(slider,{key:'Home'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('0 B');
  fireEvent.keyDown(slider,{key:'ArrowRight'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('No sample');
  fireEvent.keyDown(slider,{key:'End'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('20 B');
 });
 it(`excludes ${compact?'compact':'expanded'} buckets ending at start or beginning at exclusive end, including their scale and inspection`,()=>{
  const v=renderApp(<CapacityHistoryChart q={series([{time:base-30000,value:99999},{time:base,value:0},{time:base+30000,value:99999}])} title="Bytes" compact={compact}/>,route(0,30000));
  const dots=v.container.querySelectorAll('.tc-dot,.spark-dot');
  expect(dots).toHaveLength(1);
  expect(Number(dots[0].getAttribute('cx'))).toBe(compact?100:150);
  expect(Number(dots[0].getAttribute('cy'))).toBe(compact?37:70);
  fireEvent.keyDown(screen.getByRole('slider'),{key:'End'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('No sample');
  expect(screen.getByRole('tooltip')).not.toHaveTextContent('99');
 });
 it(`leaves ${compact?'compact':'expanded'} unavailable or empty measurements unmarked`,()=>{
  const v=renderApp(<CapacityHistoryChart q={{isPending:false,error:undefined,data:undefined}} title="Bytes" compact={compact}/>,route(15000,25000));
  expect(v.container.querySelectorAll('.tc-dot,.spark-dot,polyline')).toHaveLength(0);
  fireEvent.keyDown(screen.getByRole('slider'),{key:'Home'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('No sample');
 });
}
it('preserves unrelated renderers original partial-bucket coordinates and notes by default',()=>{
 const q=series([{time:base,value:40}]);
 const start=base+15000,end=base+25000;
 const v=renderApp(<><OneLine f={{start,end,spec:{kind:'fixed',from:start,to:end}}} q={q} title="Control" label="value"/><Sparkline points={q.data.points} stepMs={stepMs} start={start} end={end} label="compact control" status="ok" windows={[]}/></>,route(15000,25000));
 expect(Number(v.container.querySelector('.tc-dot')!.getAttribute('cx'))).toBe(-450);
 expect(v.container.querySelector('.spark polyline')!.getAttribute('points')).toMatch(/^-300.0,/);
 fireEvent.keyDown(screen.getByRole('slider',{name:'Inspect Control'}),{key:'Home'});
 expect(screen.getByRole('tooltip')).toHaveTextContent('Mean value in bucket');
 expect(screen.getByRole('tooltip')).not.toHaveTextContent('midpoint');
});

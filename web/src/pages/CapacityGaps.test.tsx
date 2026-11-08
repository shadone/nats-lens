import { expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { CapacityHistoryChart } from '../components/CapacityHistory';
import { OneLine } from '../components/Trends';
import { Sparkline } from '../charts/Sparkline';
import { HistoryContext } from '../components/HistoryNote';
import { renderApp } from '../test/render';
const start = Date.parse('2026-10-17T12:00:00Z');
const route = '/capacity?from=2026-10-17T12:00:00Z&to=2026-10-17T12:10:00Z';
for (const compact of [false, true]) for (const step of [30000, 120000]) {
 it(`splits ${compact ? 'compact' : 'expanded'} traces at absent ${step} ms buckets and inspects unknowns`, () => {
  const points = [0, 1, 3, 4, 8].map(i => ({time:start+i*step,value:0}));
  const q = {isPending:false,error:undefined,data:{metric:'stream.bytes',unit:'bytes' as const,stepMs:step,points,topPoints:[]}};
  const v = renderApp(<CapacityHistoryChart q={q} title="Logical bytes" compact={compact}/>, `/capacity?from=${new Date(start).toISOString()}&to=${new Date(start+10*step).toISOString()}`);
  const lines = [...v.container.querySelectorAll('.tc-svg polyline, .spark polyline')];
  expect(lines).toHaveLength(2);
  for (const line of lines) expect(line.getAttribute('points')?.split(' ')).toHaveLength(2);
  expect(v.container.querySelectorAll('.tc-dot, .spark-dot')).toHaveLength(1);
  const slider = screen.getByRole('slider');
  fireEvent.keyDown(slider,{key:'Home'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('0 B');
  fireEvent.keyDown(slider,{key:'ArrowRight'}); fireEvent.keyDown(slider,{key:'ArrowRight'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('No sample');
  expect(screen.getByRole('tooltip')).toHaveTextContent(step === 30000 ? '12:01:00 UTC' : '12:04:00 UTC');
  fireEvent.keyDown(slider,{key:'ArrowRight'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('0 B');
  Object.defineProperty(slider,'getBoundingClientRect',{value:()=>({left:0,width:600})});
  fireEvent.pointerMove(slider,{clientX:120});
  expect(screen.getByRole('tooltip')).toHaveTextContent('No sample');
  fireEvent.keyDown(slider,{key:'End'});
  expect(screen.getByRole('tooltip')).toHaveTextContent('No sample');
 });
}
it('keeps existing unrelated chart gap tolerance', () => {
 const points=[{time:start,value:10},{time:start+60000,value:10}];
 const q={isPending:false,error:undefined,data:{metric:'cluster.connections',unit:'count' as const,stepMs:30000,points,topPoints:[]}};
 const v=renderApp(<><OneLine f={{spec:{kind:'fixed',from:start,to:start+600000},start,end:start+600000}} q={q} title="Connections" label="connections"/><Sparkline points={points} stepMs={30000} status="ok" windows={[]} start={start} end={start+600000} label="existing"/></>,route);
 expect(v.container.querySelectorAll('.tc-svg polyline, .spark polyline')).toHaveLength(2);
});

it('retains partial boundary buckets, single samples and collection/retention overlays', () => {
 const q={isPending:false,error:undefined,data:{metric:'server.capacity_file_store_bytes',unit:'bytes' as const,stepMs:120000,points:[{time:start,value:0}],topPoints:[]}};
 const v=renderApp(<HistoryContext.Provider value={{start:start+30000,end:start+90000,live:false,historyStart:start+45000,gaps:[{kind:'noCollections',start:start+60000,end:start+90000,eventId:'',connection:''}]}}><CapacityHistoryChart q={q} title="Server bytes"/><CapacityHistoryChart q={q} title="Resource bytes" compact/></HistoryContext.Provider>, '/capacity?from=2026-10-17T12:00:30Z&to=2026-10-17T12:01:30Z');
 expect(v.container.querySelectorAll('.tc-dot, .spark-dot')).toHaveLength(2);
 expect(v.container.querySelectorAll('.dh-chart-gap')).not.toHaveLength(0);
 const slider=screen.getByRole('slider',{name:'Inspect Resource bytes'});
 fireEvent.keyDown(slider,{key:'End'});
 expect(screen.getByRole('tooltip')).toHaveTextContent('0 B');
 fireEvent.keyDown(slider,{key:'Home'});
 expect(screen.getByRole('tooltip')).toHaveTextContent('Not collected');
});

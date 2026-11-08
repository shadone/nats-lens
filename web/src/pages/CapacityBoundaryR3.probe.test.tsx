import {expect,it} from 'vitest';
import {fireEvent,screen} from '@testing-library/react';
import {CapacityHistoryChart} from '../components/CapacityHistory';
import {renderApp} from '../test/render';
import fixture from '../../../boundary-sql.json';
const route='/capacity?from=2026-10-17T12:00:15Z&to=2026-10-17T12:00:25Z';
for(const compact of [false,true]) it(`shows the actual SQL single partial bucket within ${compact?'compact':'expanded'} chart domain`,()=>{
 const q={isPending:false,error:undefined,data:{...fixture.series['stream.bytes'],unit:'bytes' as const}};
 const v=renderApp(<CapacityHistoryChart q={q} title="Logical bytes" compact={compact}/>,route);
 fireEvent.keyDown(screen.getByRole('slider'),{key:'Home'});
 expect(screen.getByRole('tooltip')).toHaveTextContent('40 B');
 const dot=v.container.querySelector('.tc-dot,.spark-dot')!;
 expect(dot).not.toBeNull();
 expect(Number(dot.getAttribute('cx'))).toBeGreaterThanOrEqual(0);
 expect(Number(dot.getAttribute('cx'))).toBeLessThanOrEqual(compact?200:300);
});

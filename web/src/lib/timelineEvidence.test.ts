import {expect,it} from 'vitest';
import {groupTimelineEvents} from './timelineEvidence';
import type {LaneEvent} from '../api/model';
const e=(id:string,time:number):LaneEvent=>({id,time,source:'nats',kind:'test',label:id,shortLabel:id});
it('retains every member of transitive visual overlap, separates noncolliding ticks and rejects outside marks',()=>{
 const input=[e('a',0),e('b',30),e('c',60),e('d',100),e('outside',1001)];const groups=groupTimelineEvents(input,0,1000,1000);expect(groups.map(g=>g.events.map(e=>e.id))).toEqual([['a','b','c'],['d']]);expect(input[0].id).toBe('a');
 expect(groupTimelineEvents([e('a',1),e('b',1),e('c',2)],0,1000,0).map(g=>g.events.length)).toEqual([2,1]);
});

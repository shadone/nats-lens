import {expect,it} from 'vitest';
import {navFor} from './nav';
it('names captured event and window pages while keeping their Overview navigation parent',()=>{
 expect(navFor('/timeline/events')).toMatchObject({path:'/',label:'Captured Timeline events'});
 expect(navFor('/timeline/windows/'+'a'.repeat(64))).toMatchObject({path:'/',label:'Timeline window evidence'});
});

import {useRef} from 'react';
import {act,fireEvent,render,screen} from '@testing-library/react';
import {afterEach,expect,it,vi} from 'vitest';
import {InfoTip} from '../components/InfoTip';
import {ChartReadout} from './ChartReadout';
afterEach(()=>vi.useRealTimers());
it('keeps help readable while moving from its trigger across the gap into the overlay',()=>{
 vi.useFakeTimers();render(<InfoTip label="Storage" text="Long storage guidance."/>);const b=screen.getByRole('button');fireEvent.mouseEnter(b.parentElement!);const tip=screen.getByRole('tooltip');fireEvent.mouseLeave(b.parentElement!);act(()=>vi.advanceTimersByTime(80));fireEvent.pointerEnter(tip);act(()=>vi.advanceTimersByTime(500));expect(screen.getByRole('tooltip')).toBeInTheDocument();fireEvent.pointerLeave(tip);act(()=>vi.advanceTimersByTime(500));expect(screen.queryByRole('tooltip')).toBeNull();
});
it('allows keyboard readers to scroll overflowing help without moving focus from the trigger',()=>{
 render(<InfoTip label="Storage" text="Long storage guidance."/>);const b=screen.getByRole('button');act(()=>b.focus());const tip=screen.getByRole('tooltip');Object.defineProperty(tip,'scrollHeight',{value:900});Object.defineProperty(tip,'clientHeight',{value:200});const scroll=vi.fn();tip.scrollBy=scroll;fireEvent.keyDown(b,{key:'PageDown'});expect(scroll).toHaveBeenCalledWith({top:160,behavior:'auto'});expect(document.activeElement).toBe(b);
});
it('hides a detached-from-view anchor inside an independently scrolled table',()=>{
 function Fixture(){const anchor=useRef<HTMLSpanElement>(null);return <div data-testid="table" style={{overflow:'auto'}}><span ref={anchor}>Plot</span><ChartReadout anchor={anchor} fraction={.5} id="preview" contentKey="sample">Observed value</ChartReadout></div>}
 const bounds=(left:number,right:number,top=20,bottom=60)=>({left,right,top,bottom,width:right-left,height:bottom-top,x:left,y:top,toJSON(){}} as DOMRect);
 const mock=vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockImplementation(function(this:HTMLElement){return this.dataset.testid==='table'?bounds(100,200):this.tagName==='SPAN'?bounds(220,250):bounds(0,300,0,100)});
 render(<Fixture/>);expect(screen.getByRole('tooltip',{hidden:true})).toHaveStyle({visibility:'hidden'});mock.mockRestore();
});

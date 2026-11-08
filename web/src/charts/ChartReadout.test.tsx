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
 function Fixture(){const anchor=useRef<HTMLSpanElement>(null);return <div data-testid="table" style={{overflowX:'auto',overflowY:'auto'}}><span ref={anchor}>Plot</span><ChartReadout anchor={anchor} fraction={.5} id="preview" contentKey="sample">Observed value</ChartReadout></div>}
 const bounds=(left:number,right:number,top=20,bottom=60)=>({left,right,top,bottom,width:right-left,height:bottom-top,x:left,y:top,toJSON(){}} as DOMRect);
 const mock=vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockImplementation(function(this:HTMLElement){return this.dataset.testid==='table'?bounds(100,200):this.tagName==='SPAN'?bounds(220,250):bounds(0,300,0,100)});
 render(<Fixture/>);expect(screen.getByRole('tooltip',{hidden:true})).toHaveStyle({visibility:'hidden'});mock.mockRestore();
});
it('clamps edge readouts independently of a narrow trigger and uses the native dialog layer',()=>{
 function Fixture(){const anchor=useRef<HTMLSpanElement>(null);return <dialog open><span ref={anchor}>Plot</span><ChartReadout anchor={anchor} fraction={1} id="edge" contentKey="sample">Long observed value</ChartReadout></dialog>}
 const rect=(left:number,top:number,width:number,height:number)=>({left,top,right:left+width,bottom:top+height,width,height,x:left,y:top,toJSON(){}} as DOMRect);
 const mock=vi.spyOn(HTMLElement.prototype,'getBoundingClientRect').mockImplementation(function(this:HTMLElement){return this.tagName==='SPAN'?rect(1000,730,10,10):this.className==='chart-readout'?rect(0,0,300,150):rect(0,0,1024,768)});
 const {unmount}=render(<Fixture/>);const tip=screen.getByRole('tooltip');expect(tip.closest('dialog')).not.toBeNull();expect(parseFloat(tip.style.left)+300).toBeLessThanOrEqual(1012);expect(parseFloat(tip.style.top)+150).toBeLessThan(730);expect(tip.style.maxWidth).toBe('1000px');unmount();expect(document.getElementById('edge')).toBeNull();mock.mockRestore();
});
it('cleans pending hover dismissal on unmount and retains pinned help until blur or Escape',()=>{
 vi.useFakeTimers();const {unmount}=render(<InfoTip label="Storage" text="Guidance"/>);const b=screen.getByRole('button');act(()=>b.focus());fireEvent.click(b);fireEvent.mouseLeave(b.parentElement!);act(()=>vi.advanceTimersByTime(500));expect(screen.getByRole('tooltip')).toBeInTheDocument();fireEvent.blur(b);expect(screen.queryByRole('tooltip')).toBeNull();fireEvent.mouseEnter(b.parentElement!);fireEvent.mouseLeave(b.parentElement!);unmount();act(()=>vi.runAllTimers());expect(document.querySelector('.chart-readout')).toBeNull();
});

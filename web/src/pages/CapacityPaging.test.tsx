import { useState } from 'react';
import { expect, it, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { useSearchParams } from 'react-router';
import { renderApp } from '../test/render';
import { ServerCards } from '../components/CapacityServers';
import { StreamsTable } from '../components/CapacityStreams';
import { prod, stream } from '../test/fixtures/capacity';
vi.mock('../api/client',()=>({lens:{getTimeSeriesBatch:vi.fn(()=>new Promise(()=>{}))}}));
for (const resources of [false,true]) it(`normalizes ${resources?'resource':'server'} pages after shrink, grow, empty and context changes`,()=>{
 function C(){
  const [count,setCount]=useState(80); const [,setParams]=useSearchParams();
  const d=prod(); d.streams=Array.from({length:count},(_,i)=>stream(`orders-${i}`));
  const servers=Array.from({length:count},(_,i)=>({...d.servers[0],name:`nats-${i}`}));
  return <><button onClick={()=>setCount(25)}>Shrink</button><button onClick={()=>setCount(80)}>Grow</button><button onClick={()=>setCount(0)}>Empty</button><button onClick={()=>setParams({system:'example-test',range:'1h'})}>Context</button>{resources?<StreamsTable d={d} answeredAt={1} live={false}/>:<ServerCards servers={servers} withoutJetstream={0}/>}</>;
 }
 renderApp(<C/>,'/capacity');
 const next=()=>fireEvent.click(screen.getByRole('button',{name:resources?'Next resource history page':'Next servers'}));
 const prev=()=>fireEvent.click(screen.getByRole('button',{name:resources?'Previous resource history page':'Previous servers'}));
 const page=(first:number,last:number)=>expect(screen.getByText(new RegExp(`${resources?'History rows':'Server cards'} ${first}–${last}`))).toBeInTheDocument();
 next();next();next(); fireEvent.click(screen.getByText('Shrink'));page(resources?17:25,25);
 fireEvent.click(screen.getByText('Grow'));page(resources?17:25,resources?32:48);
 prev();page(1,resources?16:24);
 next();fireEvent.click(screen.getByText('Empty'));fireEvent.click(screen.getByText('Grow'));page(1,resources?16:24);
 next();fireEvent.click(screen.getByText('Context'));page(1,resources?16:24);
 if(resources){next();fireEvent.change(screen.getByRole('combobox',{name:'Resource type'}),{target:{value:'kv'}});fireEvent.change(screen.getByRole('combobox',{name:'Resource type'}),{target:{value:'all'}});page(1,16);}
});

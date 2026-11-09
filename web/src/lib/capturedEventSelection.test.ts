import {expect,it} from 'vitest';
import {selectionPath,selectionFromURL,type CapturedSelection} from './capturedEventSelection';
const s:CapturedSelection={version:1,system:'shop',from:'2026-10-17T12:00:00Z',to:'2026-10-17T13:00:00Z',eventReadId:'9007199254740995',ids:['9007199254740993','9007199254740994'],limits:['NATS and collector events']};
it('shares captured membership outside the HTTP query and reopens the same fixed selection and page',()=>{
 const path=selectionPath(s,1,1),u=new URL(path,'https://example.com');expect(u.pathname).toBe('/timeline/events');expect(u.searchParams.has('ids')).toBe(false);expect(u.hash).not.toBe('');expect(selectionFromURL(u)).toEqual({selection:s,offset:1,pageSize:1});
});
it('rejects scope changes, duplicate fields, malformed/oversized membership and out-of-bounds pages',()=>{
 const path=selectionPath(s);
 for(const change of [(u:URL)=>u.searchParams.set('system','other'),(u:URL)=>u.searchParams.set('from','2026-10-17T11:00:00Z'),(u:URL)=>u.searchParams.append('system','shop'),(u:URL)=>u.searchParams.set('offset','2'),(u:URL)=>u.searchParams.set('pageSize','51'),(u:URL)=>u.hash='selection=!!',(u:URL)=>u.hash='selection='+ 'a'.repeat(90000)]){const u=new URL(path,'https://example.com');change(u);expect(()=>selectionFromURL(u)).toThrow()}
 for(const bad of [{...s,ids:['01']},{...s,ids:['1','1']},{...s,ids:[]},{...s,ids:Array.from({length:2001},(_,i)=>`${i+1}`)},{...s,limits:['unknown']},{...s,eventReadId:'-1'}])expect(()=>selectionPath(bad as CapturedSelection)).toThrow();
});
it('retains exact int64 strings for the maximum combined lane without sending them in HTTP headers',()=>{
 const full={...s,ids:Array.from({length:2000},(_,i)=>`${9223372036854775807n-BigInt(i)}`)};const u=new URL(selectionPath(full,1950),'https://example.com');expect(u.search.length).toBeLessThan(400);expect(u.hash.length).toBeLessThan(88000);expect(selectionFromURL(u).selection).toEqual(full);
});

it('copies the current selection page from the global handoff without adding a nonexistent request range',async()=>{
 const {buildInvestigationContext}=await import('./investigationContext');const u=new URL(selectionPath(s,1,1),'https://example.com');
 const c=buildInvestigationContext({origin:u.origin,pathname:u.pathname,search:u.search,hash:u.hash,system:s.system,spec:{kind:'fixed',from:Date.parse(s.from),to:Date.parse(s.to)}});
 expect(c.query).toEqual({method:'ListEventSelection',request:{ids:s.ids,eventReadId:s.eventReadId,offset:1,pageSize:1,system:s.system}});expect(c.range).toEqual({start:s.from,end:s.to});expect(c.annotation).toContain('NATS and collector events');
});

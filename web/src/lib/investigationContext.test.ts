import {describe,it,expect} from "vitest";
import {buildInvestigationContext} from "./investigationContext";
import {setServerNow} from "./clock";
const now=Date.parse("2026-10-17T14:30:00Z");
describe("portable investigation context",()=>{
 it("freezes the server clock, keeps facts separate and removes unknown URL secrets",()=>{
  setServerNow(now,Date.now(),true);
  const c=buildInvestigationContext({origin:"https://lens.example.com",pathname:"/servers/nats-0",search:"?range=1h&account=APP&token=secret&state=resolved",system:"shop",spec:{kind:"live",preset:"1h"}});
  expect(c.range).toEqual({start:"2026-10-17T13:30:00.000Z",end:"2026-10-17T14:30:00.000Z"});
  expect(c.entity).toMatchObject({type:"ENTITY_TYPE_SERVER",key:"nats-0"});
  expect(c.filters).toEqual({account:"APP",state:"resolved"});
  expect(JSON.stringify(c)).not.toContain("secret");expect(c.provenance.status).toBe("unavailable");expect(c.annotation).toBeUndefined();
 });
 it("preserves encoded bucket identity, metric and selected point without claiming a collection",()=>{
  const c=buildInvestigationContext({origin:"https://lens.example.com",pathname:"/kv-stores/APP/catalog",search:"",system:"shop",spec:{kind:"fixed",from:now-3600000,to:now},selection:{metric:"stream.bytes",point:now-60000,label:"Stored bytes"}});
  expect(c.entity).toMatchObject({type:"ENTITY_TYPE_STREAM",key:"APP/KV_catalog",stream:"KV_catalog"});expect(c.metric).toBe("stream.bytes");expect(c.point).toBe("2026-10-17T14:29:00.000Z");expect(c.provenance.status).toBe("unavailable");
 });
});
it("records effective Findings and Events defaults even when absent from the URL",()=>{
 const input={origin:"https://lens.example.com",search:"",system:"shop",spec:{kind:"live",preset:"1h"} as const};
 expect(buildInvestigationContext({...input,pathname:"/findings"}).filters).toMatchObject({state:"open",muted:"1"});
 expect(buildInvestigationContext({...input,pathname:"/events"}).filters).toMatchObject({show:"worth"});
});
it("preserves storage comparison pins and sub-millisecond requested bounds in copied context",()=>{
 const c=buildInvestigationContext({origin:"https://lens.example.com",pathname:"/capacity/storage-change",search:"?from=2026-10-17T13:00:00.000123Z&to=2026-10-17T14:00:00.000456Z&epochA=9007199254740993&epochB=9007199254740994&account=APP",system:"shop",spec:{kind:"fixed",from:Date.parse("2026-10-17T13:00:00Z"),to:Date.parse("2026-10-17T14:00:00Z")}});
 expect(c.filters.epochA).toBe("9007199254740993");expect(c.filters.epochB).toBe("9007199254740994");expect(c.range).toEqual({start:"2026-10-17T13:00:00.000123Z",end:"2026-10-17T14:00:00.000456Z"});
});
it("keeps contributor selection in the copied pinned investigation",()=>{
 const c=buildInvestigationContext({origin:"https://lens.example.com",pathname:"/capacity/storage-change",search:"?change=asc&resource=kv&contributors=1",system:"shop",spec:{kind:"fixed",from:now-3600000,to:now}});
 expect(c.filters).toMatchObject({change:"asc",resource:"kv",contributors:"1"});
});
it('copies the exact occurrence read as descriptor-validated data without turning its route suffix into a finding ID',()=>{
 const selection={check:'stream-001',query:{method:'ListFindingEpisodes',request:{findingId:'stream-001:APP/orders',firstRetainedEpoch:'9007199254740993',readEpoch:'9007199254740995',includeTransitions:true,pageSize:1}}};
 const c=buildInvestigationContext({origin:'https://lens.example.com',pathname:'/findings/stream-001%3AAPP%2Forders/occurrences/9007199254740993',search:'?readEpoch=9007199254740995',system:'shop',spec:{kind:'fixed',from:now-3600000,to:now},selection});
 expect(c).toMatchObject({query:{method:'ListFindingEpisodes',request:{system:'shop',range:c.range,firstRetainedEpoch:'9007199254740993',readEpoch:'9007199254740995',includeTransitions:true}}});
 expect(c.evidence).toContainEqual({kind:'finding',id:'stream-001:APP/orders'});
});

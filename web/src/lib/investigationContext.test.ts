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

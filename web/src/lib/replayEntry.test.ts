import { describe,it,expect } from "vitest";
import { findingReplayEntry, replayEntry, windowReplayEntry } from "./replayEntry";
import type { Finding } from "../api/model";
const now = Date.parse("2026-10-17T12:00:00Z");
const f: Finding = {id:"f",checkCode:"server-004",checkName:"Restart",severity:"warning",state:"unknown",detail:"",remediation:"",relatedConfig:[],relatedConfigSource:"",transitionsLastHour:0};
function moments(to:string) {const p=new URL(to,"http://example.com").searchParams;return {a:Date.parse(p.get("a")!),b:Date.parse(p.get("b")!),from:Date.parse(p.get("from")!),to:Date.parse(p.get("to")!)};}
describe("bounded Replay entry",()=>{
 it("normalizes a very short window to a collection interval and fixes its surrounding strip",()=>{
  const e=windowReplayEntry({start:now-1000,end:now,label:"Upgrade",detail:""},now,900000)!;
  expect(moments(e.to)).toEqual({a:now-900000,b:now,from:now-900000,to:now});
  expect(e.notes.join()).toMatch(/collect interval/);
 });
 it("caps long windows at 24 hours and reports the omitted start",()=>{
  const e=windowReplayEntry({start:now-2*86400000,end:now,label:"Upgrade",detail:""},now,30000)!;
  expect(moments(e.to).a).toBe(now-86400000);
  expect(e.notes.join()).toMatch(/24 hours/);
 });
 it("does not silently move old evidence into retention or history",()=>{
  const old=now-20*86400000;
  expect(moments(replayEntry({a:old,b:old+600000},now,30000)!.to).a).toBe(old);
 });
 it("rejects missing, reversed and future observed windows",()=>{
  for(const w of [{start:NaN,end:now},{start:now,end:now-1000},{start:now+1,end:now+1000}])
   expect(windowReplayEntry({...w,label:"Upgrade",detail:""},now,30000)).toBeUndefined();
 });
 it("never treats unknown condition timing as a transition",()=>{
  expect(findingReplayEntry({...f,conditionSince:now-1000},[{state:"unknown",severity:"warning",detail:""}],now-3600000,now,now,30000)).toBeUndefined();
 });
 it("uses first sighting only when it is inside the selected range",()=>{
  const e=findingReplayEntry({...f,since:now-600000},[],now-3600000,now,now,30000)!;
  expect(e.observation).toMatch(/first sighting/);
  expect(findingReplayEntry({...f,since:now-86400000},[],now-3600000,now,now,30000)).toBeUndefined();
 });
});

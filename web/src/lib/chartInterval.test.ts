import {expect,it} from "vitest";
import {chartInterval,intervalProblem,parseUTCInput} from "./chartInterval";
it("clamps reversed selections and rejects empty or nonfinite intervals",()=>{
 expect(chartInterval(150,-50,{start:0,end:100})).toEqual({start:0,end:100});expect(chartInterval(1,1,{start:0,end:100})).toBeUndefined();expect(chartInterval(NaN,10,{start:0,end:100})).toBeUndefined();expect(chartInterval(1,10,{start:100,end:0})).toBeUndefined();
});
it("interprets valid picker values in UTC and rejects calendar overflow",()=>{
 expect(parseUTCInput("2026-10-17T13:00:12.123")).toBe(Date.parse("2026-10-17T13:00:12.123Z"));for(const raw of ["2026-02-30T12:00","2026-13-01T12:00","2026-01-01T24:00","0000-01-01T00:00","garbage"])expect(parseUTCInput(raw)).toBeUndefined();
});
it("requires ordered bounds inside the visible frame and honors the shared range limit",()=>{
 const start=Date.parse("2026-10-17T00:00Z"),end=start+1000;expect(intervalProblem({start,end},{start,end})).toBeUndefined();expect(intervalProblem({start:start-1,end},{start,end})).toBeTruthy();expect(intervalProblem({start:end,end:start},{start,end})).toBeTruthy();expect(intervalProblem({start,end:start+31*86400000},{start,end:start+31*86400000})).toBeTruthy();
});

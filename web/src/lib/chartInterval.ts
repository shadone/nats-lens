import type {InvestigationSelection} from "./investigationContext";
import {rangeProblem} from "./timeRange";

export interface ChartInterval {start:number;end:number}
const storageMetrics=new Set(["cluster.js_storage_bytes","server.js_storage_bytes","cluster.capacity_file_store_bytes","server.capacity_file_store_bytes","stream.bytes","stream.observed_replica_bytes"]);
export function supportsStorageChange(source:InvestigationSelection|undefined){return [source?.metric,...(source?.series?.map(x=>x.metric)??[])].some(x=>!!x&&storageMetrics.has(x))}
export function validChartFrame(frame:ChartInterval){return Number.isFinite(frame.start)&&Number.isFinite(frame.end)&&frame.start<frame.end&&Number.isFinite(new Date(frame.start).getTime())&&Number.isFinite(new Date(frame.end).getTime())}
export function chartInterval(a:number,b:number,frame:ChartInterval):ChartInterval|undefined {
 if(!validChartFrame(frame)||!Number.isFinite(a)||!Number.isFinite(b))return;
 const start=Math.max(frame.start,Math.min(frame.end,Math.round(Math.min(a,b))));
 const end=Math.max(frame.start,Math.min(frame.end,Math.round(Math.max(a,b))));
 return start<end?{start,end}:undefined;
}
export function intervalProblem(range:ChartInterval|undefined,frame:ChartInterval){
 if(!range||range.start<frame.start||range.end>frame.end||range.start>=range.end)return "Choose two different times inside this chart’s interval.";
 return rangeProblem(new URLSearchParams({from:new Date(range.start).toISOString(),to:new Date(range.end).toISOString()}),frame.end);
}
export function utcInput(value:number){return new Date(value).toISOString().slice(0,-1)}
/** datetime-local is explicitly interpreted as UTC, never the browser's zone. */
export function parseUTCInput(raw:string):number|undefined {
 const m=/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?$/.exec(raw);if(!m||+m[1]<1)return;
 const n=Date.parse(raw+"Z"),d=new Date(n);if(!Number.isFinite(n)||d.getUTCFullYear()!==+m[1]||d.getUTCMonth()+1!==+m[2]||d.getUTCDate()!==+m[3]||d.getUTCHours()!==+m[4]||d.getUTCMinutes()!==+m[5]||d.getUTCSeconds()!==+(m[6]??0))return;
 return n;
}

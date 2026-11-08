import {resourceMetric,resourceSelection} from "./resourceMetrics";
import {metricFamily} from "./metricNavigation";
import {fromJson} from "@bufbuild/protobuf";
import {TimestampSchema} from "@bufbuild/protobuf/wkt";
// Portable data only: no credential, clipboard or network access here.
import type {Entity} from "../api/model";
import {serverNow} from "./clock";
import {resolveRange,type RangeSpec} from "./timeRange";
export interface InvestigationSeries {label:string;metric?:string;entity?:Entity}
export interface InvestigationSelection {check?:string;annotation?:string;metric?:string;entity?:Entity;series?:InvestigationSeries[];evidence?:{kind:string;id:string}[];start?:number;end?:number;point?:number;label?:string}
export interface InvestigationEntity {type:string;key:string;account?:string;stream?:string;consumer?:string;server?:string}
export interface InvestigationContext {
 version:1;deployment:string;system:string;account?:string;range:{start:string;end:string};sourcePath:string;
 chartLabel?:string;series?:{label:string;metric?:string;entity?:InvestigationEntity;unavailableReason?:string}[];
 filters:Record<string,string>;entity?:InvestigationEntity;metric?:string;point?:string;evidence?:{kind:string;id:string}[];
 provenance:{status:"unavailable";reason:string};annotation?:string;
}
const FILTERS="sev state muted check src kind client show type key server account stream consumer id filter q sort dir a b kv ephemeral name tooling process epoch subject cmp servers asset epochA epochB change resource contributors".split(" ");
function routeEntity(pathname:string):InvestigationEntity|undefined{
 let p:string[];try{p=pathname.split("/").slice(1).map(decodeURIComponent)}catch{return undefined}
 if(p[0]==="servers"&&p.length===2&&p[1]!=="slow-consumers")return {type:"ENTITY_TYPE_SERVER",key:p[1],server:p[1]};
 if(["streams","kv-stores","object-stores"].includes(p[0])&&p.length===3){const stream=(p[0]==="kv-stores"?"KV_":p[0]==="object-stores"?"OBJ_":"")+p[2];return{type:"ENTITY_TYPE_STREAM",key:`${p[1]}/${stream}`,account:p[1],stream}}
 if(p[0]==="consumers"&&p.length===4)return{type:"ENTITY_TYPE_CONSUMER",key:p.slice(1).join("/"),account:p[1],stream:p[2],consumer:p[3]};
 return undefined;
}
function portableEntity(e:Entity|undefined):InvestigationEntity|undefined{return e?{type:`ENTITY_TYPE_${e.kind.toUpperCase()}`,key:e.key,account:e.account||undefined,stream:e.stream||undefined,consumer:e.consumer||undefined,server:e.server||undefined}:undefined}
export function buildInvestigationContext(input:{origin:string;pathname:string;search:string;system:string;spec:RangeSpec;selection?:InvestigationSelection}):InvestigationContext {
 const {origin,pathname,search,system,spec,selection:selection}=input;
 const family=pathname.startsWith("/metrics/")?metricFamily(pathname.slice("/metrics/".length)):undefined;
 const server=new URLSearchParams(search).get("server");const serverIdentity=server?{kind:"server" as const,key:server,server,name:server,account:"",stream:"",consumer:""}:undefined;
 const resource=pathname.startsWith("/metrics/")?resourceMetric(pathname.slice("/metrics/".length)):undefined;let selectedResource:Entity|undefined;try{if(resource)selectedResource=resourceSelection(resource,new URLSearchParams(search))}catch{/* Invalid route selectors remain untrusted filters, not invented identities. */}
 const s:InvestigationSelection|undefined=selection??(resource?{label:resource.title,metric:resource.metric,entity:selectedResource}:undefined)??(family?{label:family.title,series:(server?family.metrics:family.fleetMetrics).map((metric,i)=>({label:family.labels[i],metric,entity:serverIdentity}))}:undefined);
 const params=new URLSearchParams(search);
 // ASVS 14.2.1: copy only known navigation fields; unknown query parameters,
 // credentials, cookies and browser storage never enter portable context.
 const filters:Record<string,string>={};for(const k of FILTERS){const v=params.get(k);if(v!==null&&v.length<=1024)filters[k]=v}
 if(s?.check)filters.check=s.check;
 if(pathname==="/findings"){filters.state ||= "open";filters.muted ||= "1"}
 if(pathname==="/events")filters.show ||= "worth";
 const range=resolveRange(spec,serverNow());const start=s?.start??range.start,end=s?.end??range.end;
 let portableRange={start:new Date(start).toISOString(),end:new Date(end).toISOString()};
 if(pathname==="/capacity/storage-change"&&s?.start===undefined&&s?.end===undefined){
  try{const from=params.get("from")!,to=params.get("to")!,a=fromJson(TimestampSchema,from),b=fromJson(TimestampSchema,to);if(a.seconds<b.seconds||a.seconds===b.seconds&&a.nanos<b.nanos)portableRange={start:from,end:to}}catch{/* Invalid links keep the existing resolved range and cannot invent pins. */}
 }
 const refs=s?.series??(s&&(s.metric||s.label)?[{label:s.label??"Chart",metric:s.metric,entity:s.entity}]:undefined);
 const series=refs?.map(ref=>({label:ref.label,metric:ref.metric,entity:portableEntity(ref.entity),...(ref.metric?{}:{unavailableReason:"Metric identity is unavailable from this chart data"})}));
 const primary=series?.find(ref=>ref.metric);
 let entity=portableEntity(s?.entity)??primary?.entity??routeEntity(pathname);
 // Selected chart/route identities outrank retained selectors. Query-only
 // contexts use the same consumer > stream > server precedence as Events.
 if(!entity&&pathname==="/events"&&params.get("consumer")){const key=params.get("consumer")!,p=key.split("/");if(p.length===3)entity={type:"ENTITY_TYPE_CONSUMER",key,account:p[0],stream:p[1],consumer:p[2]}}
 if(!entity&&["/events","/consumers"].includes(pathname)&&params.get("stream")){const key=params.get("stream")!,p=key.split("/");if(p.length===2)entity={type:"ENTITY_TYPE_STREAM",key,account:p[0],stream:p[1]}}
 if(!entity&&["/events","/connections","/servers/slow-consumers","/placement"].includes(pathname)&&params.get("server"))entity={type:"ENTITY_TYPE_SERVER",key:params.get("server")!,server:params.get("server")!};
 const evidence:{kind:string;id:string}[]=[...(s?.evidence??[])];
 if(pathname.startsWith("/findings/")&&pathname!=="/findings/by-labels"&&!evidence.some(e=>e.kind==="finding"))evidence.push({kind:"finding",id:decodeURIComponent(pathname.slice("/findings/".length))});
 if(pathname==="/events"&&params.get("id"))evidence.push({kind:"event",id:params.get("id")!});
 return {version:1,deployment:origin,system,account:entity?.account||params.get("account")||undefined,range:portableRange,sourcePath:pathname,filters,entity,chartLabel:s?.label,series,metric:s?.metric??primary?.metric,point:s?.point!==undefined&&s.point>=start&&s.point<end?new Date(s.point).toISOString():undefined,evidence:evidence.length?evidence.filter((e,i)=>evidence.findIndex(other=>other.kind===e.kind&&other.id===e.id)===i):undefined,annotation:s?.annotation,provenance:{status:"unavailable",reason:"API does not provide a cross-query collection pin. Follow Data history and response evidence IDs; this context records URL-backed filters only."}};
}

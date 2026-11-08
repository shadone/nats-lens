// Portable data only: no credential, clipboard or network access here.
import type {Entity} from "../api/model";
import {serverNow} from "./clock";
import {resolveRange,type RangeSpec} from "./timeRange";
export interface InvestigationSelection {metric?:string;entity?:Entity;start?:number;end?:number;point?:number;label?:string}
export interface InvestigationEntity {type:string;key:string;account?:string;stream?:string;consumer?:string;server?:string}
export interface InvestigationContext {
 version:1;deployment:string;system:string;account?:string;range:{start:string;end:string};sourcePath:string;
 filters:Record<string,string>;entity?:InvestigationEntity;metric?:string;point?:string;evidence?:{kind:string;id:string}[];
 provenance:{status:"unavailable";reason:string};annotation?:string;
}
const FILTERS="sev state muted check src kind client show type key server account stream consumer id filter q sort dir a b".split(" ");
function routeEntity(pathname:string):InvestigationEntity|undefined{
 let p:string[];try{p=pathname.split("/").slice(1).map(decodeURIComponent)}catch{return undefined}
 if(p[0]==="servers"&&p.length===2&&p[1]!=="slow-consumers")return {type:"ENTITY_TYPE_SERVER",key:p[1],server:p[1]};
 if(["streams","kv-stores","object-stores"].includes(p[0])&&p.length===3){const stream=(p[0]==="kv-stores"?"KV_":p[0]==="object-stores"?"OBJ_":"")+p[2];return{type:"ENTITY_TYPE_STREAM",key:`${p[1]}/${stream}`,account:p[1],stream}}
 if(p[0]==="consumers"&&p.length===4)return{type:"ENTITY_TYPE_CONSUMER",key:p.slice(1).join("/"),account:p[1],stream:p[2],consumer:p[3]};
 return undefined;
}
export function buildInvestigationContext(input:{origin:string;pathname:string;search:string;system:string;spec:RangeSpec;selection?:InvestigationSelection}):InvestigationContext {
 const {origin,pathname,search,system,spec,selection:s}=input;
 const params=new URLSearchParams(search);
 // ASVS 14.2.1: copy only known navigation fields; unknown query parameters,
 // credentials, cookies and browser storage never enter portable context.
 const filters:Record<string,string>={};for(const k of FILTERS){const v=params.get(k);if(v!==null&&v.length<=1024)filters[k]=v}
 if(pathname==="/findings"){filters.state ||= "open";filters.muted ||= "1"}
 if(pathname==="/events")filters.show ||= "worth";
 const range=resolveRange(spec,serverNow());const start=s?.start??range.start,end=s?.end??range.end;
 let entity=s?.entity?{type:`ENTITY_TYPE_${s.entity.kind.toUpperCase()}`,key:s.entity.key,account:s.entity.account||undefined,stream:s.entity.stream||undefined,consumer:s.entity.consumer||undefined,server:s.entity.server||undefined}:routeEntity(pathname);
 if(!entity&&params.get("server"))entity={type:"ENTITY_TYPE_SERVER",key:params.get("server")!,server:params.get("server")!};
 if(!entity&&params.get("stream")){const key=params.get("stream")!,p=key.split("/");if(p.length===2)entity={type:"ENTITY_TYPE_STREAM",key,account:p[0],stream:p[1]}}
 if(params.get("consumer")){const key=params.get("consumer")!,p=key.split("/");if(p.length===3)entity={type:"ENTITY_TYPE_CONSUMER",key,account:p[0],stream:p[1],consumer:p[2]}}
 const evidence:{kind:string;id:string}[]=[];
 if(pathname.startsWith("/findings/")&&pathname!=="/findings/by-labels")evidence.push({kind:"finding",id:decodeURIComponent(pathname.slice("/findings/".length))});
 if(pathname==="/events"&&params.get("id"))evidence.push({kind:"event",id:params.get("id")!});
 return {version:1,deployment:origin,system,account:entity?.account||params.get("account")||undefined,range:{start:new Date(start).toISOString(),end:new Date(end).toISOString()},sourcePath:pathname,filters,entity,metric:s?.metric,point:s?.point!==undefined&&s.point>=start&&s.point<end?new Date(s.point).toISOString():undefined,evidence:evidence.length?evidence:undefined,provenance:{status:"unavailable",reason:"API does not provide a cross-query collection pin. Follow Data history and response evidence IDs; this context records URL-backed filters only."}};
}

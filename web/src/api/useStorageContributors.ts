import {fromJson} from "@bufbuild/protobuf";
import {TimestampSchema,type Timestamp} from "@bufbuild/protobuf/wkt";
import {useQuery,useQueryClient} from "@tanstack/react-query";
import {useEffect,useState} from "react";
import {useSearchParams} from "react-router";
import {lens} from "./client";
import {StorageChangeOrder} from "../gen/natslens/v1/storage_change_pb";
import {CapacityStreamKind} from "../gen/natslens/v1/capacity_pb";
import {useSystem} from "../lib/useSystem";
import {useTimeRange} from "../lib/useTimeRange";
import {resolveRange} from "../lib/timeRange";

function pin(raw:string|null){if(raw===null)return 0n;if(!/^[1-9]\d{0,18}$/.test(raw)||BigInt(raw)>9223372036854775807n)throw new Error("The collection pin is invalid.");return BigInt(raw)}
export function useStorageContributors(){
 const system=useSystem();const [params,setParams]=useSearchParams();const {spec,problem:rangeProblem}=useTimeRange();
 const account=params.get("account")??"";const [generation,refresh]=useState(0);
 const base=[system,account,params.get("range"),params.get("from"),params.get("to"),params.get("epochA"),params.get("epochB"),generation].join("|");
 const clock=useQuery({queryKey:["capacity-comparison-clock",base],enabled:spec.kind==="live",staleTime:Infinity,queryFn:async({signal})=>{const r=await lens.getLensInfo({system},{signal});if(!r.now)throw new Error("Source clock is unavailable.");return Number(r.now.seconds)*1000+r.now.nanos/1e6}});
 const order=params.get("change")==="asc"?StorageChangeOrder.LOGICAL_ASCENDING:params.get("change")==="desc"?StorageChangeOrder.LOGICAL_DESCENDING:StorageChangeOrder.UNSPECIFIED;
 const type=params.get("resource");const resourceKind=type==="kv"?CapacityStreamKind.KEY_VALUE:type==="object"?CapacityStreamKind.OBJECT_STORE:type==="stream"?CapacityStreamKind.STREAM:CapacityStreamKind.UNSPECIFIED;
 let problem=rangeProblem;let a:Timestamp|undefined,b:Timestamp|undefined;let initialA=0n,initialB=0n;
 try{
  initialA=pin(params.get("epochA"));initialB=pin(params.get("epochB"));
  if(spec.kind==="fixed"){a=fromJson(TimestampSchema,params.get("from"));b=fromJson(TimestampSchema,params.get("to"))}
  else if(clock.data!==undefined&&!clock.isFetching&&!clock.isError){const r=resolveRange(spec,clock.data);a=fromJson(TimestampSchema,new Date(r.start).toISOString());b=fromJson(TimestampSchema,new Date(r.end).toISOString())}
 }catch(e){problem=e instanceof Error?e.message:"Invalid comparison link"}
 const [snapshot,setSnapshot]=useState({base,system,a:initialA,b:initialB});const snap=snapshot.base===base?snapshot:{base,system,a:initialA,b:initialB};
 const selection=[base,order,resourceKind].join("|");const [paging,setPaging]=useState({selection,token:"",previous:[] as string[],offset:0});const page=paging.selection===selection?paging:{selection,token:"",previous:[],offset:0};
 const key=(sys:string,pa:bigint,pb:bigint,token:string)=>["capacity-contributors",selection,sys,String(pa),String(pb),token];
 const q=useQuery({queryKey:key(snap.system,snap.a,snap.b,page.token),enabled:!problem&&!!a&&!!b,staleTime:Infinity,queryFn:({signal})=>lens.getStorageChange({system:snap.system,account,a,b,epochA:snap.a,epochB:snap.b,pageSize:100,pageToken:page.token,order,resourceKind,resourcesOnly:true},{signal})});
 const d=q.data;
 const resolved={base,system:snap.system||d?.system||system,a:d?.a?.epoch||snap.a,b:d?.b?.epoch||snap.b};
 const normalizing=!!d&&(snap.system!==resolved.system||snap.a!==resolved.a||snap.b!==resolved.b);
 const qc=useQueryClient();
 useEffect(()=>{if(!d||!normalizing)return;qc.setQueryData(key(resolved.system,resolved.a,resolved.b,page.token),d);setSnapshot(resolved)},[d,normalizing,resolved.system,resolved.a,resolved.b,base,selection,page.token,qc]);
 return {selection,q,problem:problem||(clock.isError?String(clock.error):undefined),ready:!!a&&!!b,order,resourceKind,type,page,normalizing,a,b,system:resolved.system,account,params,setParams,
  next:()=>{if(d?.nextPageToken&&!normalizing)setPaging({selection,token:d.nextPageToken,previous:[...page.previous,page.token],offset:page.offset+d.rows.length})},
  previous:()=>{const previous=[...page.previous];const token=previous.pop();if(token!==undefined)setPaging({selection,token,previous,offset:Math.max(0,page.offset-100)})},
  refresh:()=>refresh(x=>x+1)};
}

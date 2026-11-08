// Metric semantics come from the API catalogue, not a parallel UI registry.
import {useQuery} from "@tanstack/react-query";
import {lens} from "../api/client";
import {InfoTip} from "./MetricBox";

function Description({ids}:{ids:string[]}) {
  const q=useQuery({queryKey:["metric-catalogue"],queryFn:({signal})=>lens.listMetrics({}, {signal}),staleTime:Infinity,retry:false});
  if(q.isPending)return <>Loading metric description…</>;
  if(q.error)return <>Metric description unavailable. The chart data is independent of this help request.</>;
  return <>{ids.map(id=>{const metric=q.data.metrics.find(m=>m.id===id);return <p key={id}>{metric?.description||`Description unavailable for ${id}.`}</p>})}</>;
}

/** Catalogue reads start on inspection and share one cached response across charts. */
export function MetricHelp({label,ids}:{label:string;ids:string[]}) {
  const unique=[...new Set(ids.filter(Boolean))];
  return <InfoTip label={label} text={unique.length?<Description ids={unique}/>:"Metric identity is unavailable for this chart. Inspect its labels and evidence before interpreting the values."}/>;
}

import {toJson} from "@bufbuild/protobuf";
import {TimestampSchema,type Timestamp} from "@bufbuild/protobuf/wkt";
import type {StorageChangeRow,StorageChangeEndpoint,GetStorageChangeResponse} from "../gen/natslens/v1/storage_change_pb";
import {serverPath,streamPath} from "./view";
export const stamp = (t: Timestamp | undefined) => t ? String(toJson(TimestampSchema, t)) : "Unknown";
export const bytes = (v: bigint | undefined, signed = false) => v === undefined ? "Unknown" : `${v < 0n ? "−" : signed && v > 0n ? "+" : ""}${(v < 0n ? -v : v).toLocaleString("en-US")} bytes`;
const reasons: Record<string, string> = {
  no_comparable_endpoints:"Separate fresh collection windows are required to measure a change; retained values alone do not establish change in this interval",
  resource_inventory_incomplete:"Detailed resource reports do not match the server-reported inventory; totals are unknown",
 exclusive_end_inclusion:"The captured source uses its disclosed one-microsecond exclusive end; observations stop at the original source clock",offline_ui_unavailable:"No remote UI serves this local dataset",
 no_collection: "No retained collection at or before this moment", stale_collection: "The selected collection is older than three collection intervals",
  missing_jsz: "Missing JSZ evidence; complete storage totals are unavailable", server_usage_is_system_wide: "Server usage covers all accounts; it cannot be reconciled with this account's resources",
  observed_delete_event: "A recorded deletion qualifies absence; it does not establish the cause of other changes", incarnation_replaced: "A later creation time establishes a replaced incarnation",
  not_observed_at_a: "Not observed at A; absence is not zero", not_observed_at_b: "Not observed at B; absence is not zero", incarnation_unknown: "Creation identity is unknown; endpoint values are not a comparable incarnation",
  invalid_creation_time: "Reported creation time exceeds its collection window", created_after_a: "Reported creation time is after A", configured_replicas_changed: "Configured replicas differ between the endpoints",
  replica_coverage_incomplete: "Not every configured copy has a usable report", ambiguous_configuration_or_identity: "Configuration or resource identity is ambiguous", leader_or_copy_ambiguity: "Leader or copy reports disagree",
  no_leader_consistent_copies: "No leader report; logical size uses agreeing copy reports", server_measurement_unavailable: "Server measurement is unavailable", server_process_changed: "A different server process reports this name at B",
};
export function reason(x: string) { return reasons[x] ?? x.replaceAll("_", " "); }

export function resourceHistoryPath(row:StorageChangeRow,data:GetStorageChangeResponse,system:string,account:string,from:string,to:string){
 const endpoint=row.processesB.length||!row.processesA.length?data.b:data.a;
 return resourceEndpointPath(row,endpoint,system,account,from,to);
}
export function resourceEndpointPath(row:StorageChangeRow,endpoint:StorageChangeEndpoint|undefined,system:string,account:string,from:string,to:string){
 if(endpoint?.startedAt&&endpoint.finishedAt){
  from=stamp(endpoint.startedAt);
  // Keep the existing detail-link whole-second inclusion convention. Use the
  // smallest exclusive-end tick; exact source windows remain above.
  to=new Date((Number(endpoint.finishedAt.seconds)+1)*1000).toISOString().replace(".000Z","Z");
 }
 const q=new URLSearchParams({system,account:row.account||account,from,to});
 return `${row.rowKind==="server"?serverPath(row.server):streamPath(row.account,row.stream)}?${q}`;
}

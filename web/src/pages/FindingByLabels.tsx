// Query parameters decode Prometheus urlquery's + as space, exactly once.
import { Navigate, useSearchParams } from "react-router";
import { useFinding } from "../api/queries";
import { QueryView } from "../components/common";
import { useSystem } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";
import { findingPath } from "../lib/view";
import { Findings } from "./Findings";
export function FindingByLabels() {
  const [params] = useSearchParams();
  const system = useSystem();
  const code = params.get("code") ?? "";
  const key = params.get("entity_key") ?? "";
  const { spec } = useTimeRange();
  const q = useFinding(spec, `${code}:${key}`);
  return <QueryView q={q} what="the finding from its labels">{d => d.finding ?
    // ASVS 1.2.2: findingPath encodes the ID as one path segment.
    <Navigate replace to={`${findingPath(d.finding.id)}?${new URLSearchParams({ system })}`} /> :
    <Findings labelLookup={{ code, key }} />}</QueryView>;
}

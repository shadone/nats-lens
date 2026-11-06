// Query parameters decode Prometheus urlquery's + as space, exactly once.
import { Navigate, useSearchParams } from "react-router";
import { errorInfo } from "../api/errors";
import { useFinding } from "../api/queries";
import { QueryView } from "../components/common";
import { useGlobalQuery } from "../lib/useGlobalQuery";
import { useTimeRange } from "../lib/useTimeRange";
import { findingPath } from "../lib/view";
export function FindingByLabels() {
  const [params] = useSearchParams();
  const global = useGlobalQuery();
  const fallbackParams = new URLSearchParams(global);
  const code = params.get("code") ?? "";
  const key = params.get("entity_key") ?? "";
  const { spec } = useTimeRange();
  const q = useFinding(spec, `${code}:${key}`);
  fallbackParams.set("check", code);
  const fallback = <Navigate replace to={`/findings?${fallbackParams}`} state={{ labelLookup: { code, key } }} />;
  if (q.error && errorInfo(q.error).code === "not_found") return fallback;
  return <QueryView q={q} what="the finding from its labels">{d => d.finding ?
    // ASVS 1.2.2: findingPath encodes the ID as one path segment.
    <Navigate replace to={`${findingPath(d.finding.id)}${global}`} /> :
    fallback}</QueryView>;
}

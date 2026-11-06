// Findings describe the selected range, not an instantaneous RTT threshold.
import { useQuery } from "@tanstack/react-query";
import type { Finding } from "./model";
import { listFindings } from "./requests";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";

/** One bounded batch, shared by every row and page in this collection.
 * No placeholder: a different range or newest collection must not inherit status. */
export function useConnectionFindings(spec: RangeSpec, epoch: string | undefined) {
  const system = useSystem();
  return useQuery({
    queryKey: ["connectionFindings", system, rangeKey(spec), epoch],
    enabled: epoch !== undefined && epoch !== "0",
    queryFn: ({ signal }) => listFindings(system, spec, {
      includeMuted: true, pageSize: 500, states: ["firing", "changed", "flapping", "resolved"],
    }, signal),
    refetchInterval: spec.kind === "live" ? LIVE_REFRESH_MS : false,
  });
}

/** Match the checks' actual identity, not a substring or a pod prefix.
 * Auth failures (client-006) identify a credential, not a connection name. */
export function connectionFindings(findings: Finding[], name: string): Finding[] {
  const prefix = name.split("@", 1)[0];
  const service = prefix || "(unnamed)";
  const full = name || "(unnamed)";
  return findings.filter(f => {
    if (f.entity?.kind !== "client") return false;
    switch (f.checkCode) {
      case "client-001":
      case "client-008": return f.entity.key === service;
      case "client-002":
      case "client-003":
      case "client-004":
      case "client-005": return f.entity.key === full;
      case "client-007": return prefix === "" && f.entity.key === "(unnamed)";
      default: return false;
    }
  });
}

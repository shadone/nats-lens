// What a finding's page reads beyond the finding: each server's copy of
// its stream or consumer, and how many other entities share the finding.
import { useQuery } from "@tanstack/react-query";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import type { Entity, Finding } from "./model";
import { getConsumer, getStream, listFindings } from "./requests";

/** Copies only streams and consumers have; nothing for other entities. */
export function hasCopies(e: Entity | undefined): e is Entity {
  return !!e && ((e.kind === "stream" && !!e.stream) || (e.kind === "consumer" && !!e.stream && !!e.consumer));
}

function refetch(spec: RangeSpec) {
  return spec.kind === "live" ? LIVE_REFRESH_MS : false;
}

/** The stream or consumer behind a finding, for its per-server table. */
export function useCopies(spec: RangeSpec, e: Entity | undefined) {
  const sys = useSystem();
  const on = hasCopies(e);
  return useQuery({
    queryKey: ["findingCopies", sys, rangeKey(spec), on ? `${e.kind}:${e.key}` : ""],
    queryFn: async ({ signal }) => {
      const x = e!;
      if (x.kind === "consumer") return { kind: "consumer" as const, servers: (await getConsumer(sys, spec, x.account, x.stream, x.consumer, signal)).servers };
      return { kind: "stream" as const, servers: (await getStream(sys, spec, x.account, x.stream, signal)).servers };
    },
    enabled: on,
    refetchInterval: refetch(spec),
  });
}

/** Findings of the same check on other entities, muted ones included. */
export function useOtherFindings(spec: RangeSpec, f: Finding | undefined) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["findingOthers", sys, rangeKey(spec), f?.checkCode, f?.id],
    queryFn: async ({ signal }) => {
      // Without states the API lists every state but resolved.
      const page = await listFindings(sys, spec, { checkCode: f!.checkCode, includeMuted: true, pageSize: 1 }, signal);
      return Math.max(0, page.totalCount - (f!.state === "resolved" ? 0 : 1));
    },
    enabled: f !== undefined,
    refetchInterval: refetch(spec),
  });
}

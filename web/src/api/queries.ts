// TanStack Query hooks over requests.ts. Every hook reads the page's system
// with useSystem(), sends it, and keys on it as the second key element, so
// two systems never share a cache entry. A live range refetches on the
// collector's interval; a fixed range never changes and is not refetched.
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import type { Entity } from "./model";
import * as api from "./requests";
import { useCurrentSystem } from "./systems";

function refetch(spec: RangeSpec) {
  return spec.kind === "live" ? LIVE_REFRESH_MS : false;
}

function entityKey(e: Entity | undefined) {
  return e ? `${e.kind}:${e.key}` : "";
}

export function useOverview(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["overview", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.getOverview(sys, spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useFindings(spec: RangeSpec, f: api.FindingFilter) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["findings", sys, rangeKey(spec), { ...f, entity: entityKey(f.entity) }],
    queryFn: ({ signal }) => api.listFindings(sys, spec, f, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useFinding(spec: RangeSpec, id: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["finding", sys, id],
    queryFn: ({ signal }) => api.getFinding(sys, id, signal),
    refetchInterval: refetch(spec),
  });
}

export function useMutes(includeInactive: boolean, pageToken = "") {
  const sys = useSystem();
  return useQuery({
    queryKey: ["mutes", sys, includeInactive, pageToken],
    queryFn: ({ signal }) => api.listMutes(sys, includeInactive, pageToken, signal),
    placeholderData: keepPreviousData,
  });
}

/** Mute and unmute change findings, tallies, streams and the timeline. */
function useInvalidateAll() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries();
}

/**
 * A mute names its system in the call, the one the dialog captured when it
 * opened, so a URL that changes meanwhile cannot send it elsewhere.
 */
export function useMuteFinding() {
  const invalidate = useInvalidateAll();
  return useMutation({ mutationFn: (m: api.MuteInput & { system: string }) => api.muteFinding(m.system, m), onSuccess: invalidate });
}

export function useUnmuteFinding() {
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (v: { system: string; muteId: string; unmutedBy: string }) => api.unmuteFinding(v.system, v.muteId, v.unmutedBy),
    onSuccess: invalidate,
  });
}

export function useTimeline(spec: RangeSpec, entity?: Entity) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["timeline", sys, rangeKey(spec), entityKey(entity)],
    queryFn: ({ signal }) => api.getTimeline(sys, spec, entity, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useEvents(spec: RangeSpec, f: api.EventFilter) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["events", sys, rangeKey(spec), { ...f, entity: entityKey(f.entity) }],
    queryFn: ({ signal }) => api.listEvents(sys, spec, f, signal),
    // Only the newest page follows live data; older pages are keyset pages.
    refetchInterval: f.pageToken ? false : refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useEvent(id: string | undefined) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["event", sys, id],
    queryFn: ({ signal }) => api.getEvent(sys, id!, signal),
    enabled: id !== undefined,
    staleTime: Infinity,
  });
}

export function useStreams(spec: RangeSpec, includeKv = false) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["streams", sys, rangeKey(spec), includeKv],
    queryFn: ({ signal }) => api.listStreams(sys, spec, includeKv, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useStream(spec: RangeSpec, account: string, stream: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["stream", sys, rangeKey(spec), account, stream],
    queryFn: ({ signal }) => api.getStream(sys, spec, account, stream, signal),
    refetchInterval: refetch(spec),
  });
}

export function useConsumers(spec: RangeSpec, f: { account?: string; stream?: string; includeEphemeral?: boolean }) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["consumers", sys, rangeKey(spec), f],
    queryFn: ({ signal }) => api.listConsumers(sys, spec, f, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useConsumer(spec: RangeSpec, account: string, stream: string, name: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["consumer", sys, rangeKey(spec), account, stream, name],
    queryFn: ({ signal }) => api.getConsumer(sys, spec, account, stream, name, signal),
    refetchInterval: refetch(spec),
  });
}

export function useServers(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["servers", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.listServers(sys, spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useServer(spec: RangeSpec, name: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["server", sys, rangeKey(spec), name],
    queryFn: ({ signal }) => api.getServer(sys, spec, name, signal),
    refetchInterval: refetch(spec),
  });
}

export function useConnections(spec: RangeSpec, f: api.ConnectionFilter) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["connections", sys, rangeKey(spec), f],
    queryFn: ({ signal }) => api.listConnections(sys, spec, f, signal),
    refetchInterval: f.pageToken ? false : refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useSeries(spec: RangeSpec, metric: string, entity: Entity | undefined, maxPoints = 200) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["series", sys, rangeKey(spec), metric, entityKey(entity), maxPoints],
    queryFn: ({ signal }) => api.getSeries(sys, spec, metric, entity, maxPoints, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

/** Facts about this instance and the system; they change only on a restart. */
export function useLensInfo() {
  const sys = useSystem();
  return useQuery({ queryKey: ["lensInfo", sys], queryFn: ({ signal }) => api.getLensInfo(sys, signal), staleTime: 5 * 60_000 });
}

/** Shell status, live whatever the page's range; refreshed every collect interval. */
export function useHeaderStatus(intervalMs: number, enabled = true) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["headerStatus", sys, intervalMs],
    queryFn: ({ signal }) => api.getHeaderStatus(sys, intervalMs, signal),
    refetchInterval: intervalMs,
    refetchIntervalInBackground: false,
    enabled,
  });
}

/**
 * The shell's status call, on the collect interval of the page's system;
 * made only once the system is known to be configured.
 */
export function useShellStatus() {
  const info = useLensInfo().data;
  return useHeaderStatus(info?.collectIntervalMs ?? 30_000, useCurrentSystem().known === true);
}

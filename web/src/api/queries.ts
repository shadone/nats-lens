// TanStack Query hooks over requests.ts. A live range refetches on the
// collector's interval; a fixed range never changes and is not refetched.
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { rangeKey, type RangeSpec } from "../lib/timeRange";
import type { Entity } from "./model";
import * as api from "./requests";

export const LIVE_REFRESH_MS = 30_000;

function refetch(spec: RangeSpec) {
  return spec.kind === "live" ? LIVE_REFRESH_MS : false;
}

function entityKey(e: Entity | undefined) {
  return e ? `${e.kind}:${e.key}` : "";
}

export function useOverview(spec: RangeSpec) {
  return useQuery({
    queryKey: ["overview", rangeKey(spec)],
    queryFn: ({ signal }) => api.getOverview(spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useFindings(spec: RangeSpec, f: api.FindingFilter) {
  return useQuery({
    queryKey: ["findings", rangeKey(spec), { ...f, entity: entityKey(f.entity) }],
    queryFn: ({ signal }) => api.listFindings(spec, f, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useFinding(spec: RangeSpec, id: string) {
  return useQuery({
    queryKey: ["finding", id],
    queryFn: ({ signal }) => api.getFinding(id, signal),
    refetchInterval: refetch(spec),
  });
}

export function useMutes(includeInactive: boolean, pageToken = "") {
  return useQuery({
    queryKey: ["mutes", includeInactive, pageToken],
    queryFn: ({ signal }) => api.listMutes(includeInactive, pageToken, signal),
    placeholderData: keepPreviousData,
  });
}

/** Mute and unmute change findings, tallies, streams and the timeline. */
function useInvalidateAll() {
  const qc = useQueryClient();
  return () => qc.invalidateQueries();
}

export function useMuteFinding() {
  const invalidate = useInvalidateAll();
  return useMutation({ mutationFn: api.muteFinding, onSuccess: invalidate });
}

export function useUnmuteFinding() {
  const invalidate = useInvalidateAll();
  return useMutation({
    mutationFn: (v: { muteId: string; unmutedBy: string }) => api.unmuteFinding(v.muteId, v.unmutedBy),
    onSuccess: invalidate,
  });
}

export function useTimeline(spec: RangeSpec, entity?: Entity) {
  return useQuery({
    queryKey: ["timeline", rangeKey(spec), entityKey(entity)],
    queryFn: ({ signal }) => api.getTimeline(spec, entity, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useEvents(spec: RangeSpec, f: api.EventFilter) {
  return useQuery({
    queryKey: ["events", rangeKey(spec), { ...f, entity: entityKey(f.entity) }],
    queryFn: ({ signal }) => api.listEvents(spec, f, signal),
    // Only the newest page follows live data; older pages are keyset pages.
    refetchInterval: f.pageToken ? false : refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useEvent(id: string | undefined) {
  return useQuery({
    queryKey: ["event", id],
    queryFn: ({ signal }) => api.getEvent(id!, signal),
    enabled: id !== undefined,
    staleTime: Infinity,
  });
}

export function useStreams(spec: RangeSpec, includeKv = false) {
  return useQuery({
    queryKey: ["streams", rangeKey(spec), includeKv],
    queryFn: ({ signal }) => api.listStreams(spec, includeKv, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useStream(spec: RangeSpec, account: string, stream: string) {
  return useQuery({
    queryKey: ["stream", rangeKey(spec), account, stream],
    queryFn: ({ signal }) => api.getStream(spec, account, stream, signal),
    refetchInterval: refetch(spec),
  });
}

export function useConsumers(spec: RangeSpec, f: { account?: string; stream?: string; includeEphemeral?: boolean }) {
  return useQuery({
    queryKey: ["consumers", rangeKey(spec), f],
    queryFn: ({ signal }) => api.listConsumers(spec, f, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useConsumer(spec: RangeSpec, account: string, stream: string, name: string) {
  return useQuery({
    queryKey: ["consumer", rangeKey(spec), account, stream, name],
    queryFn: ({ signal }) => api.getConsumer(spec, account, stream, name, signal),
    refetchInterval: refetch(spec),
  });
}

export function useServers(spec: RangeSpec) {
  return useQuery({
    queryKey: ["servers", rangeKey(spec)],
    queryFn: ({ signal }) => api.listServers(spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useServer(spec: RangeSpec, name: string) {
  return useQuery({
    queryKey: ["server", rangeKey(spec), name],
    queryFn: ({ signal }) => api.getServer(spec, name, signal),
    refetchInterval: refetch(spec),
  });
}

export function useConnections(spec: RangeSpec, f: api.ConnectionFilter) {
  return useQuery({
    queryKey: ["connections", rangeKey(spec), f],
    queryFn: ({ signal }) => api.listConnections(spec, f, signal),
    refetchInterval: f.pageToken ? false : refetch(spec),
    placeholderData: keepPreviousData,
  });
}

export function useSeries(spec: RangeSpec, metric: string, entity: Entity | undefined, maxPoints = 200) {
  return useQuery({
    queryKey: ["series", rangeKey(spec), metric, entityKey(entity), maxPoints],
    queryFn: ({ signal }) => api.getSeries(spec, metric, entity, maxPoints, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

/** Facts about this instance; they change only on a restart. */
export function useLensInfo() {
  return useQuery({ queryKey: ["lensInfo"], queryFn: ({ signal }) => api.getLensInfo(signal), staleTime: 5 * 60_000 });
}

/** Header status, live whatever the page's range; refreshed every collect interval. */
export function useHeaderStatus(intervalMs: number) {
  return useQuery({
    queryKey: ["headerStatus", intervalMs],
    queryFn: ({ signal }) => api.getHeaderStatus(intervalMs, signal),
    refetchInterval: intervalMs,
    refetchIntervalInBackground: false,
  });
}

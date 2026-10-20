// TanStack Query hooks over requests.ts. Every hook reads the page's system
// with useSystem(), sends it, and keys on it as the second key element, so
// two systems never share a cache entry. A live range refetches on the
// collector's interval; a fixed range never changes and is not refetched.
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import type { Entity } from "./model";
import type { Matrix, MatrixFilter } from "./placementMatrix";
import { reportedError } from "./errors";
import * as api from "./requests";
import { shellQuery } from "./shell";

/** How long the names that fill a filter are kept before a mount reads them again. */
export const FILTER_NAMES_STALE_MS = 5 * 60_000;

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

/**
 * The streams that fill a filter. Not polled, whatever the range, and not
 * read again on window focus. Read again when a page using it mounts more
 * than five minutes after the last successful read, when the network comes
 * back after that, when the system or range changes, or on a reload. A
 * failed read keeps the streams last read; with none, the next mount tries
 * again.
 */
export function useStreamNames(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["streamNames", sys, rangeKey(spec)],
    queryFn: async ({ signal }) => (await api.listStreams(sys, spec, false, signal)).streams.map((s) => s.entity),
    refetchInterval: false,
    staleTime: FILTER_NAMES_STALE_MS,
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

/**
 * The placement matrix of a range; enabled false leaves it unread. A new
 * range shows the previous range's data while it loads; with keepFor set,
 * only when the previous query was asked with the same keepFor, so keepFor
 * names everything the kept data must share (system, page range, choice).
 */
export function usePlacement(spec: RangeSpec, filter: MatrixFilter, enabled = true, keepFor?: string) {
  const sys = useSystem();
  return useQuery<Matrix, Error, Matrix, readonly unknown[]>({
    queryKey: keepFor === undefined ? ["placement", sys, rangeKey(spec), filter] : ["placement", sys, rangeKey(spec), filter, keepFor],
    queryFn: ({ signal }) => api.getPlacement(sys, spec, filter, signal),
    refetchInterval: refetch(spec),
    placeholderData:
      keepFor === undefined
        ? keepPreviousData
        : (prev, prevQuery) => (prevQuery?.queryKey[4] === keepFor ? prev : undefined),
    enabled,
  });
}

/** The topology of the range's newest collection; a live range follows it as Placement does. */
export function useTopology(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["topology", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.getTopology(sys, spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
  });
}

/** The Accounts page: the range's newest collection, followed on a live range as Topology is. */
export function useAccountsAndAuth(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["accounts-and-auth", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.getAccountsAndAuth(sys, spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepPreviousData,
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

/**
 * The last data before the collector stopped at since. Kept for ten minutes
 * per stop, so only the first refresh after a stop pays for the lookup.
 */
export function useLastData(since: number | undefined, intervalMs: number) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["lastData", sys, since, intervalMs],
    queryFn: ({ signal }) => api.getLastData(sys, since!, intervalMs, signal),
    staleTime: 10 * 60_000,
    enabled: since !== undefined,
  });
}

/**
 * The shell's status query, with a failure of the system's own part, which
 * the response reports beside the systems, as the query's error.
 */
function useShellQuery() {
  const status = useQuery(shellQuery(useSystem()));
  const e = status.data?.error;
  const error = useMemo(() => (e ? reportedError(e.code, e.message) : null), [e?.code, e?.message]);
  return error ? { ...status, error } : status;
}

/**
 * The shell's status of the page's system. A failure of the system's own
 * part is the status's error, as a failed call's would be. While the collector does not
 * collect and the last two intervals hold no collection, the servers and
 * the newest collection come from the last data before the stop.
 */
export function useShellStatus() {
  const status = useShellQuery();
  const f = status.data?.freshness;
  const stopped = f && !f.collecting && f.epochAt === undefined ? f.notCollectingSince : undefined;
  const last = useLastData(stopped, f?.intervalMs || LIVE_REFRESH_MS).data;
  if (!status.data || !f || stopped === undefined || !last?.freshness) return status;
  const lf = last.freshness;
  const data = { ...status.data, servers: last.servers, freshness: { ...f, epoch: lf.epoch, epochAt: lf.epochAt, ageMs: lf.ageMs, partial: lf.partial } };
  return { ...status, data };
}

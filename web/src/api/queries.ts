// TanStack Query hooks over requests.ts. Every hook reads the page's system
// with useSystem(), sends it, and keys on it as the second key element, so
// two systems never share a cache entry; a previous answer stands in while
// the next key loads only for the same system (keepSameSystem). A live
// range refetches on the collector's interval; a fixed range never
// changes and is not refetched.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { keepSameSystem } from "./placeholder";
import { useMemo } from "react";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import type { Entity } from "./model";
import type { Matrix, MatrixFilter } from "./placementMatrix";
import type { SubjectFilter } from "./subjects";
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

/** Share annotations only within the exact system/range; never borrow another range. */
export function useTimeWindows(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["time-windows", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.getTimeWindows(sys, spec, signal),
    refetchInterval: refetch(spec),
    staleTime: spec.kind === "live" ? LIVE_REFRESH_MS : Infinity,
  });
}

export function useOverview(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["overview", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.getOverview(sys, spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys),
  });
}

export function useFindings(spec: RangeSpec, f: api.FindingFilter) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["findings", sys, rangeKey(spec), { ...f, entity: entityKey(f.entity) }],
    queryFn: ({ signal }) => api.listFindings(sys, spec, f, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys),
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
    placeholderData: keepSameSystem(sys),
  });
}

// These families read stored observations or metadata, not current operator
// state. Unknown same-system families remain conservatively invalidated so a
// new status-bearing view does not silently retain a stale mute or acknowledgement.
const operatorIndependent = new Set([
  "time-windows", "series", "trends", "bandSeries", "metric-ranking", "lensInfo", "data-history",
  "events", "event", "serverEvents", "timeline-events", "timeline-window", "captured-event-selection",
  "replay", "topology", "placement",
]);
// Acknowledge writes finding.acknowledged to events atomically; a mute/release does not.
const operatorEventReads = new Set(["events", "serverEvents", "data-history"]);
function operatorStateFilter(system: string, writesEvent = false) {
  return { predicate: (q: { queryKey: readonly unknown[] }) =>
    q.queryKey[0] === "systemSummaries" ||
    q.queryKey[1] === system && (writesEvent && operatorEventReads.has(String(q.queryKey[0])) || !operatorIndependent.has(String(q.queryKey[0]))) };
}
/** Keep historical/current operator-dependent reads and the global fleet tally coherent. */
function useInvalidateOperatorState() {
  const qc = useQueryClient();
  return (system: string) => qc.invalidateQueries(operatorStateFilter(system));
}

/**
 * A mute names its system in the call, the one the dialog captured when it
 * opened, so a URL that changes meanwhile cannot send it elsewhere. The
 * mutation stays pending until affected active queries have refetched, so a dialog
 * stays busy and its opener's button cannot mute twice; awaitRefetch false
 * ends it once the mute is saved, for the accept dialog, whose page shows
 * the saved acceptance until the refetch lands.
 */
export function useMuteFinding({ awaitRefetch = true }: { awaitRefetch?: boolean } = {}) {
  const invalidate = useInvalidateOperatorState();
  return useMutation({
    mutationFn: (m: api.MuteInput & { system: string }) => api.muteFinding(m.system, m),
    onSuccess: (_data, variables) => (awaitRefetch ? invalidate(variables.system) : void invalidate(variables.system)),
  });
}

export function useUnmuteFinding() {
  const invalidate = useInvalidateOperatorState();
  return useMutation({
    mutationFn: (v: { system: string; muteId: string; unmutedBy: string }) => api.unmuteFinding(v.system, v.muteId, v.unmutedBy),
    onSuccess: (_data, variables) => invalidate(variables.system),
  });
}

export function useTimeline(spec: RangeSpec, entity?: Entity, includeOk=false) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["timeline", sys, rangeKey(spec), entityKey(entity), includeOk],
    queryFn: ({ signal }) => api.getTimeline(sys, spec, entity, signal, includeOk),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys),
  });
}

export function useEvents(spec: RangeSpec, f: api.EventFilter) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["events", sys, rangeKey(spec), { ...f, entity: entityKey(f.entity) }],
    queryFn: ({ signal }) => api.listEvents(sys, spec, f, signal),
    // Only the newest page follows live data; older pages are keyset pages.
    refetchInterval: f.pageToken ? false : refetch(spec),
    placeholderData: keepSameSystem(sys),
  });
}

export function useEvent(id: string | undefined,readId?:string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["event", sys, id,readId],
    queryFn: ({ signal }) => api.getEvent(sys, id!, signal,readId),
    enabled: id !== undefined,
    staleTime: Infinity,
  });
}

export function useStreams(spec: RangeSpec, includeKv = false, pageToken = "") {
  const sys = useSystem();
  return useQuery({
    queryKey: ["streams", sys, rangeKey(spec), includeKv, pageToken],
    queryFn: ({ signal }) => api.listStreams(sys, spec, includeKv, signal, { pageToken }),
    refetchInterval: pageToken ? false : refetch(spec),
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
    placeholderData: keepSameSystem(sys),
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

export function useConsumers(spec: RangeSpec, f: { account?: string; stream?: string; includeEphemeral?: boolean }, pageToken = "") {
  const sys = useSystem();
  return useQuery({
    // The account has its own key position, so no other account's rows stand in.
    queryKey: ["consumers", sys, rangeKey(spec), f.account ?? "", f, pageToken],
    queryFn: ({ signal }) => api.listConsumers(sys, spec, f, signal, { pageToken }),
    refetchInterval: pageToken ? false : refetch(spec),
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
 * range shows the previous range's data of the same system while it
 * loads; with keepFor set, only when the previous query was asked with
 * the same keepFor, so keepFor names everything the kept data must share
 * (system, page range, choice).
 */
export function usePlacement(spec: RangeSpec, filter: MatrixFilter, enabled = true, keepFor?: string, selected?: { asset: string; server: string }) {
  const sys = useSystem();
  return useQuery<Matrix, Error, Matrix, readonly unknown[]>({
    queryKey: ["placement", sys, rangeKey(spec), filter, keepFor, selected?.asset, selected?.server],
    queryFn: ({ signal }) => selected ? api.getPlacement(sys, spec, filter, signal, selected) : api.getPlacement(sys, spec, filter, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys, keepFor === undefined ? {} : { 4: keepFor }),
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
    placeholderData: keepSameSystem(sys),
  });
}

/** The Accounts page: the range's newest collection, followed on a live range as Topology is. */
export function useAccountsAndAuth(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["accounts-and-auth", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.getAccountsAndAuth(sys, spec, signal),
    refetchInterval: refetch(spec),
    // The previous range's answer stands in while the next loads, never another system's.
    placeholderData: keepSameSystem(sys),
  });
}

/** The Capacity page: the range's newest collection, followed on a live range as Accounts is. */
export function useCapacity(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["capacity", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.getCapacity(sys, spec, signal),
    refetchInterval: refetch(spec),
    // The previous range's answer stands in while the next loads, never another system's.
    placeholderData: keepSameSystem(sys),
  });
}

/** The Data history read's cache key; `server` is empty for the whole system. */
function dataHistoryKey(sys: string, spec: RangeSpec, server: string) {
  return ["data-history", sys, rangeKey(spec), server] as const;
}
/** Every Data history key of a system, server-scoped or not. */
export const dataHistoryPrefix = (sys: string) => ["data-history", sys] as const;
/** Whether a Data history key is scoped to one server. */
export function isServerHistoryKey(key: readonly unknown[]): boolean {
  return key[0] === "data-history" && typeof key[3] === "string" && key[3] !== "";
}

/** The Data history page: the history and the range's gaps, followed on a live range as Capacity is. */
export function useDataHistory(spec: RangeSpec, server = "") {
  const sys = useSystem();
  return useQuery({
    queryKey: dataHistoryKey(sys, spec, server),
    queryFn: ({ signal }) => api.getDataHistory(sys, spec, signal, server),
    refetchInterval: refetch(spec),
    // Later chart mounts reuse the settled answer until the next live refresh.
    staleTime: spec.kind === "live" ? LIVE_REFRESH_MS : Infinity,

    // Gaps open at one range end say nothing about a different range.
    // Keep cached answers only for this exact system and range key.
  });
}

/**
 * The Subjects page: the range's newest collection, followed on a live
 * range as Capacity is. The previous answer stands in while the next
 * loads only for the same system and account, so a filter change keeps
 * the pills' counts but no other account's rows show.
 */
export function useSubjects(spec: RangeSpec, account: string, filter: SubjectFilter) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["subjects", sys, rangeKey(spec), account, filter],
    queryFn: ({ signal }) => api.getSubjects(sys, spec, account, filter, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys, { 3: account }),
  });
}

/**
 * The Micro services page: the range's newest collection. The previous
 * answer stands in while the next loads only for the same system and
 * account, so a switch never shows another account's services.
 */
export function useMicroServices(spec: RangeSpec, account: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["microServices", sys, rangeKey(spec), account],
    queryFn: ({ signal }) => api.getMicroServices(sys, spec, account, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys, { 3: account }),
  });
}

/**
 * The Applications page: the range's newest collection. The previous
 * answer stands in while the next loads only for the same system and
 * account, so a switch never shows another account's applications; a
 * change of the tooling toggle keeps it.
 */
export function useApplications(spec: RangeSpec, account: string, includeTooling: boolean) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["applications", sys, rangeKey(spec), account, includeTooling],
    queryFn: ({ signal }) => api.getApplications(sys, spec, account, includeTooling, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys, { 3: account }),
  });
}

/**
 * What a message on subject would reach; "" asks nothing. No answer
 * stands in for another subject's while the next loads.
 */
export function useMatchSubject(spec: RangeSpec, account: string, subject: string) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["matchSubject", sys, rangeKey(spec), account, subject],
    queryFn: ({ signal }) => api.matchSubject(sys, spec, account, subject, signal),
    refetchInterval: refetch(spec),
    enabled: subject !== "",
  });
}

export function useServers(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["servers", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.listServers(sys, spec, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys),
  });
}

export function useConnections(spec: RangeSpec, f: api.ConnectionFilter) {
  const sys = useSystem();
  return useQuery({
    // The account has its own key position, so no other account's rows stand in.
    queryKey: ["connections", sys, rangeKey(spec), f.account ?? "", f],
    queryFn: ({ signal }) => api.listConnections(sys, spec, f, signal),
    refetchInterval: f.pageToken ? false : refetch(spec),
    placeholderData: keepSameSystem(sys, { 3: f.account ?? "" }),
  });
}

export function useSeries(spec: RangeSpec, metric: string, entity: Entity | undefined, maxPoints = 200) {
  const sys = useSystem();
  const query=useQuery({
    queryKey: ["series", sys, rangeKey(spec), metric, entityKey(entity), maxPoints],
    queryFn: ({ signal }) => api.getSeries(sys, spec, metric, entity, maxPoints, signal),
    refetchInterval: refetch(spec),
    placeholderData: keepSameSystem(sys,{3:metric,4:entityKey(entity)}),
  });
  return {...query,metric,entity};
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

/** A saved acknowledgement appears immediately in both current detail caches.
 * Refetch this system’s operator-dependent evidence and the shared fleet tallies. */
export function useSaveAcknowledgement() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { system: string; episodeSince?: number } & ({ action: "ack"; input: api.AckInput } | { action: "release"; id: string; endedBy: string })) =>
      v.action === "ack" ? api.acknowledgeFinding(v.system, v.input) : api.endAcknowledgement(v.system, v.id, v.endedBy),
    // ASVS 15.4.2: capture the episode and revision before the asynchronous write;
    // a response cannot revive superseded state or overwrite newer ownership.
    onMutate: v => {
      const snapshots = new Map<string, { revision: number; finding: import("./model").Finding }>();
      for (const kind of ["finding", "oncall"]) {
        for (const q of qc.getQueryCache().findAll({ queryKey: [kind, v.system] })) {
          const f = (q.state.data as { finding?: import("./model").Finding } | undefined)?.finding;
          if (f && (v.episodeSince === undefined || f.since === v.episodeSince) && (v.action === "ack" ? f.id === v.input.findingId : f.acknowledgement?.id === v.id))
            snapshots.set(q.queryHash, { revision: q.state.dataUpdateCount, finding: f });
        }
      }
      return snapshots;
    },
    onSuccess: (a, v, snapshots) => {
      const filter = operatorStateFilter(v.system, v.action === "ack");
      for (const kind of ["finding", "oncall"]) {
        const key = [kind, v.system, a.findingId];
        const q = qc.getQueryCache().find({ queryKey: key, exact: true });
        const before = q && snapshots?.get(q.queryHash);
        const f = (q?.state.data as { finding?: import("./model").Finding } | undefined)?.finding;
        // A strictly later server acknowledgement may advance existing ownership.
        // An absent acknowledgement after a read/release remains authoritative.
        const advancesAck = a.endedAt === undefined && f?.acknowledgement !== undefined &&
          (a.createdAt ?? -Infinity) > (f.acknowledgement.createdAt ?? Infinity);
        if (!q || !before || !f || (q.state.dataUpdateCount !== before.revision && !advancesAck) ||
            f.id !== before.finding.id || f.since === undefined || f.since !== before.finding.since ||
            f.state !== before.finding.state || f.state === "resolved" || f.state === "unknown" ||
            (f.acknowledgement?.createdAt ?? -Infinity) > (a.createdAt ?? -Infinity) ||
            (v.action === "release" && f.acknowledgement?.id !== v.id)) continue;
        qc.setQueryData(key, (d: { finding?: import("./model").Finding } | undefined) =>
          d ? { ...d, finding: { ...f, acknowledgement: a.endedAt === undefined ? a : undefined } } : d);
      }
      void qc.invalidateQueries(filter);
    },
  });
}

export function useOnCall(id: string) {
  const sys = useSystem();
  return useQuery({ queryKey: ["oncall", sys, id], queryFn: ({ signal }) => api.getOnCall(sys, id, signal), refetchInterval: LIVE_REFRESH_MS });
}

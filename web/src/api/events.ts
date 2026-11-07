// The Events page's feed: routine events hidden, repeats folded, facets
// and the histogram, plus the probe behind the "N new" pill and the
// events around a finding's start. Every hook keys on the page's system.
import { useQuery } from "@tanstack/react-query";
import { keepSameSystem } from "./placeholder";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import type { Event as PEvent, ListEventsResponse } from "../gen/natslens/v1/events_pb";
import { serverNow } from "../lib/clock";
import { LIVE_REFRESH_MS, presetMs, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { ms, sourceEnum } from "./adapt-common";
import { event } from "./adapt-findings";
import { lens } from "./client";
import type { Entity, EventSourceName, LensEvent } from "./model";
import { entityInit, systemInit } from "./requests";

/** An event of a feed. With folding, one row stands for repeats events. */
export interface FeedEvent extends LensEvent {
  /** Events the row stands for; 1 without folding. */
  repeats: number;
  /** The oldest event's time of a folded row; the row's time is the newest. */
  firstTime?: number;
  routine: boolean;
}

export interface RoutineGroup {
  /** Text from the API, shown as sent. */
  label: string;
  kind: string;
  clientName: string;
  count: number;
}

export interface HistogramBucket {
  start: number;
  warning: number;
  notable: number;
  routine: number;
}

export interface EventFeed {
  items: FeedEvent[];
  nextPageToken: string;
  /** Events, not rows, that match the filters. */
  totalCount: number;
  routineHidden: number;
  routineGroups: RoutineGroup[];
  sources: { source: EventSourceName; count: number }[];
  servers: { server: string; count: number }[];
  histogram: HistogramBucket[];
  totalUnfiltered: number;
  /** The range the feed read; a live range ends when it was read. */
  start: number;
  end: number;
}

export interface FeedFilter {
  sources: EventSourceName[];
  kinds: string[];
  entity?: Entity;
  clientName: string;
  /** Hide routine events and fold repeats. */
  worthReading: boolean;
  pageSize: number;
  pageToken: string;
}

/** Bars of the histogram over the range. */
export const HISTOGRAM_BUCKETS = 48;

const SOURCE_VALUE: Record<string, EventSourceName> = {
  EVENT_SOURCE_NATS: "nats",
  EVENT_SOURCE_KUBERNETES: "kubernetes",
  EVENT_SOURCE_FINDING: "finding",
  EVENT_SOURCE_COLLECTOR: "collector",
};

export function feedEvent(e: PEvent): FeedEvent {
  return { ...event(e), repeats: e.repeats || 1, firstTime: ms(e.firstTime), routine: e.routine };
}

/** The request fields every feed call shares. */
function filterInit(f: FeedFilter) {
  return {
    sources: f.sources.map(sourceEnum),
    kinds: f.kinds,
    entity: entityInit(f.entity),
    clientName: f.clientName,
    hideRoutine: f.worthReading,
    foldRepeats: f.worthReading,
  };
}

export function eventFeed(r: ListEventsResponse, start: number, end: number): EventFeed {
  const sources: EventFeed["sources"] = [];
  const servers: EventFeed["servers"] = [];
  for (const f of r.facets) {
    if (f.dimension === "source" && SOURCE_VALUE[f.value]) sources.push({ source: SOURCE_VALUE[f.value], count: f.count });
    else if (f.dimension === "server" && f.value) servers.push({ server: f.value, count: f.count });
  }
  return {
    items: r.events.map(feedEvent),
    nextPageToken: r.nextPageToken,
    totalCount: r.totalCount,
    routineHidden: r.routineHidden,
    routineGroups: r.routineGroups.map((g) => ({ label: g.label, kind: g.kind, clientName: g.clientName, count: g.count })),
    sources,
    servers,
    histogram: r.histogram.map((b) => ({ start: ms(b.start) ?? 0, warning: b.warning, notable: b.notable, routine: b.routine })),
    totalUnfiltered: r.totalUnfiltered,
    start,
    end,
  };
}

/** One page of the feed, with facets and the histogram. */
export async function listEventFeed(sys: string, spec: RangeSpec, f: FeedFilter, signal?: AbortSignal, now = serverNow()): Promise<EventFeed> {
  const start = spec.kind === "live" ? now - presetMs(spec.preset) : spec.from;
  const end = spec.kind === "live" ? now : spec.to;
  const r = await lens.listEvents(
    {
      ...systemInit(sys),
      // A live range leaves the end open, so the server reads up to its now.
      range: spec.kind === "live" ? { start: timestampFromMs(start) } : { start: timestampFromMs(start), end: timestampFromMs(end) },
      ...filterInit(f),
      histogramBuckets: HISTOGRAM_BUCKETS,
      includeFacets: true,
      pageSize: f.pageSize,
      pageToken: f.pageToken,
    },
    { signal },
  );
  return eventFeed(r, start, end);
}

/** Events matching f from start to now: the probe behind the "N new" pill. */
export async function countEventsSince(sys: string, start: number, f: FeedFilter, signal?: AbortSignal): Promise<number> {
  const r = await lens.listEvents(
    { ...systemInit(sys), range: { start: timestampFromMs(start) }, ...filterInit(f), foldRepeats: false, pageSize: 1, pageToken: "" },
    { signal },
  );
  return r.totalCount;
}

/** Events worth reading from start to end, repeats folded. */
export async function listEventsBetween(sys: string, start: number, end: number, entity?: Entity, signal?: AbortSignal): Promise<{ items: FeedEvent[]; totalCount: number }> {
  const r = await lens.listEvents(
    {
      ...systemInit(sys),
      range: { start: timestampFromMs(start), end: timestampFromMs(end) },
      // A client is filtered by its connection name, as the Events page does.
      ...(entity?.kind === "client" ? { clientName: entity.key } : { entity: entityInit(entity) }),
      hideRoutine: true,
      foldRepeats: true,
      pageSize: AROUND_ROWS,
    },
    { signal },
  );
  return { items: r.events.map(feedEvent), totalCount: r.totalCount };
}

function feedKey(f: FeedFilter) {
  return { ...f, entity: f.entity ? `${f.entity.kind}:${f.entity.key}` : "" };
}

/**
 * The feed as the page shows it. It is not refetched on a timer or on
 * focus, so rows never move while they are read; useNewEvents tells when
 * there is more, and refetching the key shows it.
 */
export function useEventFeed(spec: RangeSpec, f: FeedFilter) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["eventFeed", sys, rangeKey(spec), feedKey(f)],
    queryFn: ({ signal }) => listEventFeed(sys, spec, f, signal),
    // Never stale: going back to page 1 shows it as read, not refetched.
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    placeholderData: keepSameSystem(sys),
  });
}

/**
 * How many events matching f arrived since the shown first page was read:
 * a fresh count from the shown page's start to now, less the shown count.
 * Only a live range's first page asks.
 */
export function useNewEvents(spec: RangeSpec, f: FeedFilter, shown: EventFeed | undefined) {
  const sys = useSystem();
  const enabled = spec.kind === "live" && f.pageToken === "" && shown !== undefined;
  const q = useQuery({
    queryKey: ["eventFeedNew", sys, rangeKey(spec), feedKey(f), shown?.start, shown?.totalCount],
    queryFn: ({ signal }) => countEventsSince(sys, shown!.start, f, signal),
    enabled,
    refetchInterval: LIVE_REFRESH_MS,
    refetchIntervalInBackground: false,
    // The first answer comes one interval after the page was read.
    initialData: shown?.totalCount,
    initialDataUpdatedAt: () => Date.now(),
    staleTime: LIVE_REFRESH_MS,
  });
  if (!enabled || q.data === undefined || shown === undefined) return 0;
  return Math.max(0, q.data - shown.totalCount);
}

/** The ten minutes either side of t: what happened around a finding's start. */
export const AROUND_MS = 10 * 60_000;

/** Rows "Around the start" shows at most. */
export const AROUND_ROWS = 20;

/** Events around t, for the whole system or, given one, for an entity. */
export function useEventsAround(t: number | undefined, entity?: Entity) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["eventsAround", sys, t, entity ? `${entity.kind}:${entity.key}` : ""],
    queryFn: ({ signal }) => listEventsBetween(sys, t! - AROUND_MS, t! + AROUND_MS, entity, signal),
    enabled: t !== undefined,
    // Loaded once: only the finding and its timeline follow a live range.
    staleTime: Infinity,
  });
}

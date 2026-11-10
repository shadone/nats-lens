// The findings and events of the command bar: one small page of each,
// filtered by text on the server. A query key per text, so a stale request
// is cancelled when the text changes and its result is never shown for the
// new text; while the next key loads, the previous rows stand in only for
// the same system, range and narrowing.
import { useQuery } from "@tanstack/react-query";
import { serverNow } from "../lib/clock";
import { rangeKey, resolveRange, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { searchEvents, type FeedEvent } from "./events";
import type { Finding, Page } from "./model";
import { keepSameSystem } from "./placeholder";
import { listFindings } from "./requests";
import { useCurrentSystem } from "./systems";

/** Characters of text before findings and events are asked for. */
export const MIN_REMOTE_TEXT = 2;

export interface RemoteSearch {
  text: string;
  /** Last part of the selected range to read; unset reads all of it. */
  sinceMs?: number;
  /** Largest number of rows to read; it differs when the box is narrowed to one kind. */
  limit: number;
}

/** The selected range, or its last sinceMs. */
export function searchRange(spec: RangeSpec, sinceMs: number | undefined, now = serverNow()): RangeSpec {
  if (sinceMs === undefined) return spec;
  const { start, end } = resolveRange(spec, now);
  return { kind: "fixed", from: Math.max(start, end - sinceMs), to: end };
}

function useRemote<T>(family: string, spec: RangeSpec, s: RemoteSearch, enabled: boolean,
  load: (sys: string, range: RangeSpec, signal: AbortSignal) => Promise<Page<T>>) {
  const sys = useSystem();
  const known = useCurrentSystem().known;
  const range = rangeKey(spec);
  return useQuery({
    queryKey: [family, sys, range, s.text, s.sinceMs ?? 0, s.limit],
    queryFn: ({ signal }) => load(sys, searchRange(spec, s.sinceMs), signal),
    enabled: enabled && known === true,
    staleTime: 30_000,
    placeholderData: keepSameSystem(sys, { 2: range, 5: s.limit }),
  });
}

/** Findings of the range whose check or entity contains the text, muted ones too. */
export function useFindingMatches(spec: RangeSpec, s: RemoteSearch, enabled: boolean) {
  return useRemote<Finding>("command-findings", spec, s, enabled, (sys, range, signal) =>
    listFindings(sys, range, { text: s.text, includeMuted: true, pageSize: s.limit }, signal));
}

/** Events of the range whose kind, reason or names contain the text, as the Events page shows them. */
export function useEventMatches(spec: RangeSpec, s: RemoteSearch, enabled: boolean) {
  return useRemote<FeedEvent>("command-events", spec, s, enabled, (sys, range, signal) => searchEvents(sys, range, s.text, s.limit, signal));
}

// The Replay page's hooks: the story between A and B, and the status strip
// of the range the moments are picked on. Every hook keys on the page's
// system.
import { useQuery } from "@tanstack/react-query";
import { LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useDebounced } from "../lib/useDebounced";
import { useSystem } from "../lib/useSystem";
import type { Replay } from "./adapt-replay";
import * as api from "./requests";

/** How long A and B stay still before the replay is asked for, so a moving handle asks once. */
export const REPLAY_DEBOUNCE_MS = 300;

type Key = readonly [string, string, number | undefined, number | undefined];

/**
 * The moments once they have rested for REPLAY_DEBOUNCE_MS, so a moving
 * handle asks for one replay, not one per step. The first moments come at
 * once, and so does a change of system: another system's page never asks
 * for the moments of the one before.
 */
export function useSettledMoments(a: number | undefined, b: number | undefined): { a?: number; b?: number } {
  const sys = useSystem();
  const key = useDebounced(JSON.stringify([sys, a ?? null, b ?? null]), REPLAY_DEBOUNCE_MS, (settled, next) => {
    const [ss, sa] = JSON.parse(settled) as [string, number | null];
    return ss !== (JSON.parse(next) as [string])[0] || sa === null;
  });
  const [ks, ka, kb] = JSON.parse(key) as [string, number | null, number | null];
  if (ks !== sys || ka === null || kb === null) return {};
  return { a: ka, b: kb };
}

/**
 * The replay between a and b, unread while either is unknown; pass the
 * settled moments. While the next one loads, the previous one of the same
 * system stays as placeholder, and its asked moments say which it is;
 * another system's never stays. No refetch interval: the moments are fixed.
 */
export function useReplay(a: number | undefined, b: number | undefined) {
  const sys = useSystem();
  const m = { a, b };
  return useQuery<Replay, Error, Replay, Key>({
    queryKey: ["replay", sys, m.a, m.b],
    queryFn: ({ signal }) => api.getReplay(sys, m.a!, m.b!, signal),
    enabled: m.a !== undefined && m.b !== undefined,
    placeholderData: (prev, prevQuery) => (prevQuery?.queryKey[1] === sys ? prev : undefined),
    refetchOnWindowFocus: false,
  });
}

/** The status timeline of the range with every check, OK ones included, for the scrubber's strip. */
export function useReplayStrip(spec: RangeSpec) {
  const sys = useSystem();
  return useQuery({
    queryKey: ["replayStrip", sys, rangeKey(spec)],
    queryFn: ({ signal }) => api.getTimeline(sys, spec, undefined, signal, true),
    refetchInterval: spec.kind === "live" ? LIVE_REFRESH_MS : false,
  });
}

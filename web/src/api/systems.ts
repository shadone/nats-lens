// The systems nats-lens observes, their Fleet summaries, and the accounts
// of one system.
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import type { ListSystemsResponse } from "../gen/natslens/v1/systems_pb";
import { DEFAULT_PRESET, LIVE_REFRESH_MS, rangeKey, type RangeSpec } from "../lib/timeRange";
import { useSystem } from "../lib/useSystem";
import { systemSummary } from "./adapt-systems";
import { lens } from "./client";
import type { SystemSummary } from "./model";
import { rangeInit, systemInit } from "./requests";

/** Cells in each system's status strip. */
export const STRIP_CELLS = 48;

export interface SystemInfo {
  id: string;
  name: string;
  environment: string;
}

export interface Systems {
  systems: SystemInfo[];
  /** The system a request without ?system= reads. */
  defaultSystem: string;
}

export interface AccountInfo {
  account: string;
  name: string;
  isSystem: boolean;
  connections?: number;
  streams?: number;
  consumers?: number;
}

/** The systems of a ListSystems response, without their summaries. */
export function systemsOf(r: ListSystemsResponse): Systems {
  const systems = r.systems.flatMap((s) => (s.system ? [{ id: s.system.id, name: s.system.name || s.system.id, environment: s.system.environment }] : []));
  return { systems, defaultSystem: r.defaultSystem || systems[0]?.id || "" };
}

export async function listAccounts(sys: string, spec: RangeSpec, signal?: AbortSignal): Promise<AccountInfo[]> {
  const r = await lens.listAccounts({ ...systemInit(sys), range: rangeInit(spec) }, { signal });
  return r.accounts.map((a) => ({
    account: a.account,
    name: a.accountName,
    isSystem: a.isSystem,
    connections: a.connections,
    streams: a.streams,
    consumers: a.consumers,
  }));
}

/** The range the system list and the sidebar's Fleet badge read. */
const DEFAULT_LIVE: RangeSpec = { kind: "live", preset: DEFAULT_PRESET };

/**
 * The configured systems; they change only on a restart. They come from
 * the query of the default live summaries, so that a page load asks
 * ListSystems once for both.
 */
export function useSystems() {
  return useQuery({ ...summariesQuery(DEFAULT_LIVE), select: systemsOf });
}

/**
 * The system the page shows: its ?system=, else the default one. known is
 * false for a ?system= that names no configured system.
 */
export function useCurrentSystem(): { id: string; info?: SystemInfo; known: boolean | undefined; systems?: Systems } {
  const param = useSystem();
  const systems = useSystems().data;
  const id = param || systems?.defaultSystem || "";
  const info = systems?.systems.find((s) => s.id === id);
  // Without ?system= the server reads its default system, which always exists.
  const known = !param ? true : systems ? info !== undefined : undefined;
  return { id, info, known, systems };
}

/** The accounts of the page's system; asked only once the system is known to be configured. */
export function useAccounts(spec: RangeSpec) {
  const sys = useSystem();
  const known = useCurrentSystem().known;
  return useQuery({
    queryKey: ["accounts", sys, rangeKey(spec)],
    queryFn: ({ signal }) => listAccounts(sys, spec, signal),
    enabled: known === true,
    staleTime: 60_000,
  });
}

function summariesOf(r: ListSystemsResponse): SystemSummary[] {
  return r.systems.map(systemSummary);
}

/** The ListSystems query of a range; a live range is asked again on the live interval. */
function summariesQuery(spec: RangeSpec) {
  return {
    queryKey: ["systemSummaries", rangeKey(spec)],
    queryFn: ({ signal }: { signal: AbortSignal }) => lens.listSystems({ range: rangeInit(spec), stripBuckets: STRIP_CELLS }, { signal }),
    refetchInterval: spec.kind === "live" ? LIVE_REFRESH_MS : (false as const),
    refetchIntervalInBackground: false,
  };
}

/**
 * Every system's summary over the range, in configuration order. Calls
 * that arrive while one for the same range is pending share its result,
 * in the browser and in the server.
 */
export function useSystemSummaries(spec: RangeSpec) {
  return useQuery({ ...summariesQuery(spec), select: summariesOf, placeholderData: keepPreviousData });
}

/** The summaries of the default live range, whatever the page shows: the sidebar's Fleet badge. */
export function useLiveSystemSummaries() {
  return useSystemSummaries(DEFAULT_LIVE);
}

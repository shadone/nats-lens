// The systems nats-lens observes, their Fleet summaries, and the accounts
// of one system.
import { keepPreviousData, useQuery } from "@tanstack/react-query";
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

export async function listSystems(signal?: AbortSignal): Promise<Systems> {
  const r = await lens.listSystems({}, { signal });
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

/** The configured systems; they change only on a restart. */
export function useSystems() {
  return useQuery({ queryKey: ["systems"], queryFn: ({ signal }) => listSystems(signal), staleTime: 60_000 });
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

export async function listSystemSummaries(spec: RangeSpec, signal?: AbortSignal): Promise<SystemSummary[]> {
  const r = await lens.listSystems({ range: rangeInit(spec), stripBuckets: STRIP_CELLS }, { signal });
  return r.systems.map(systemSummary);
}

/**
 * Every system's summary over the range, in configuration order. A live
 * range is asked again on the live interval; a call still pending is
 * awaited, never started a second time.
 */
export function useSystemSummaries(spec: RangeSpec) {
  return useQuery({
    queryKey: ["systemSummaries", rangeKey(spec)],
    queryFn: ({ signal }) => listSystemSummaries(spec, signal),
    refetchInterval: spec.kind === "live" ? LIVE_REFRESH_MS : false,
    refetchIntervalInBackground: false,
    placeholderData: keepPreviousData,
  });
}

/** The summaries of the default live range, whatever the page shows: the sidebar's Fleet badge. */
export function useLiveSystemSummaries() {
  return useSystemSummaries({ kind: "live", preset: DEFAULT_PRESET });
}

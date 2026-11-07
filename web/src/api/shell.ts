// The shell's one status query per system, shared by the header, the
// sidebar, the system switcher and the system gates.
import type { Query } from "@tanstack/react-query";
import { LIVE_REFRESH_MS } from "../lib/timeRange";
import type { ShellStatus } from "./model";
import * as api from "./requests";

/**
 * The live status of a system, whatever the page's range, asked again on
 * the system's collect interval once the first answer names it.
 */
export function shellQuery(sys: string) {
  return {
    queryKey: ["shell", sys],
    queryFn: ({ signal }: { signal: AbortSignal }) => api.getShellStatus(sys, undefined, signal),
    refetchInterval: (q: Query<ShellStatus, Error, ShellStatus, string[]>) => q.state.data?.freshness?.intervalMs || LIVE_REFRESH_MS,
    refetchIntervalInBackground: false,
  };
}

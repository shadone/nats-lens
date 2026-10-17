// The global parameters of the current page, for links to other pages. A
// provider works them out once per URL change; every link reads the result.
import { createContext, useContext, useMemo, type ReactNode } from "react";
import { useSystems } from "../api/systems";
import { globalQuery } from "./globalQuery";
import { rangeKey } from "./timeRange";
import { useAccountScope, useSystem } from "./useSystem";
import { useTimeRange } from "./useTimeRange";

const GlobalQueryContext = createContext<string | null>(null);

/**
 * The range, system and account of the page as a query string. A page
 * without ?system= in a nats-lens with several systems shows the default
 * system; its links name that system, so they never depend on the default.
 * A ?system= that names no configured system is left out, so the sidebar
 * leads out of the "system not found" page.
 */
function useComputedGlobalQuery(): string {
  const { spec } = useTimeRange();
  const param = useSystem();
  const [account] = useAccountScope();
  const systems = useSystems().data;
  const known = !systems || systems.systems.some((s) => s.id === param);
  const fallback = systems && systems.systems.length > 1 ? systems.defaultSystem : "";
  const system = param && known ? param : fallback;
  const acct = param && !known ? "" : account;
  return useMemo(() => globalQuery(spec, system, acct), [rangeKey(spec), system, acct]);
}

export function GlobalQueryProvider({ children }: { children: ReactNode }) {
  return <GlobalQueryContext.Provider value={useComputedGlobalQuery()}>{children}</GlobalQueryContext.Provider>;
}

/** The global query string of the page; needs a GlobalQueryProvider inside the router. */
export function useGlobalQuery(): string {
  const q = useContext(GlobalQueryContext);
  if (q === null) throw new Error("useGlobalQuery needs a GlobalQueryProvider");
  return q;
}

// The global parameters of the current page, for links to other pages.
import { useSystems } from "../api/systems";
import { globalQuery } from "./globalQuery";
import { useAccountScope, useSystem } from "./useSystem";
import { useTimeRange } from "./useTimeRange";

/**
 * The range, system and account of the page as a query string. A page
 * without ?system= in a nats-lens with several systems shows the default
 * system; its links name that system, so they never depend on the default.
 * A ?system= that names no configured system is left out, so the sidebar
 * leads out of the "system not found" page.
 */
export function useGlobalQuery(): string {
  const { spec } = useTimeRange();
  const param = useSystem();
  const [account] = useAccountScope();
  const systems = useSystems().data;
  const known = !systems || systems.systems.some((s) => s.id === param);
  const fallback = systems && systems.systems.length > 1 ? systems.defaultSystem : "";
  const system = param && known ? param : fallback;
  return globalQuery(spec, system, param && !known ? "" : account);
}

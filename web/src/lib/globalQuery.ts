// The parameters every page shares: the time range, the system and the
// account scope. Links between pages carry them, so a link opens the next
// page on the same system, range and account.
import { writeRange, type RangeSpec } from "./timeRange";

export const SYSTEM_PARAM = "system";
export const ACCOUNT_PARAM = "account";

/** "?range=6h&system=a&account=APP", or "" when nothing is set. */
export function globalQuery(spec: RangeSpec, system: string, account: string): string {
  const p = writeRange(new URLSearchParams(), spec);
  if (system) p.set(SYSTEM_PARAM, system);
  if (account) p.set(ACCOUNT_PARAM, account);
  const q = p.toString();
  return q ? `?${q}` : "";
}

/**
 * The link with the global parameters added; a parameter the link sets
 * itself wins. A link to another system leaves the account scope behind:
 * accounts belong to one system.
 */
export function withGlobal(to: string, global: string): string {
  const [path, own] = to.split("?");
  const params = new URLSearchParams(own ?? "");
  const g = new URLSearchParams(global);
  const otherSystem = params.has(SYSTEM_PARAM) && params.get(SYSTEM_PARAM) !== g.get(SYSTEM_PARAM);
  g.forEach((v, k) => {
    if (params.has(k) || (otherSystem && k === ACCOUNT_PARAM)) return;
    params.set(k, v);
  });
  const q = params.toString();
  return q ? `${path}?${q}` : path;
}

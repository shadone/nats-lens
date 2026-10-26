// The placeholder a system-scoped query shows while its next key loads.

/** Where a system-scoped query key holds its system: ["name", sys, ...]. */
export const SYSTEM_KEY_INDEX = 1;

/**
 * placeholderData that keeps the previous answer while the next key loads
 * only when the previous query was for the same system, and for the same
 * value at each position of also (for example { 3: account }); otherwise
 * nothing stands in. Proposed, not yet confirmed: every system-scoped
 * hook keys its system at SYSTEM_KEY_INDEX.
 */
export function keepSameSystem(sys: string, also: Record<number, unknown> = {}) {
  const want = Object.entries({ ...also, [SYSTEM_KEY_INDEX]: sys }).map(([i, v]) => [Number(i), v] as const);
  return <T>(prev: T | undefined, prevQuery?: { queryKey: readonly unknown[] }): T | undefined =>
    prevQuery && want.every(([i, v]) => prevQuery.queryKey[i] === v) ? prev : undefined;
}

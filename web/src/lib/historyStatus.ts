// The words and timing every chart and page use for the state of a Data
// history read, so the rows, the page note and the live regions agree.

export const HISTORY_LOADING = "History gaps loading";
export const HISTORY_UNAVAILABLE = "History gaps unavailable";
/** The page's one visible line when the read fails and the charts keep their rows. */
export const HISTORY_UNAVAILABLE_NOTE = "History gaps unavailable: these charts do not mark gaps.";
/** The short text for a row shared with other data. */
export const HISTORY_LOADING_SHORT = "Gaps loading";
/** How long a read is pending before a row or the page says it is loading. */
export const LOADING_DELAY_MS = 300;

/** A history read's state as the UI says it: loading, failed, or nothing to say. */
export type HistoryReadState = "loading" | "failed" | undefined;

/**
 * A query without data that refetches drops its error and goes back to
 * pending, so a read whose last outcome was an error stays failed until an
 * answer arrives; otherwise every retry would say "loading" for a moment.
 */
export function readState(r: { error: unknown; isPending: boolean; errorUpdateCount: number }): HistoryReadState {
  return r.error || (r.isPending && r.errorUpdateCount > 0) ? "failed" : r.isPending ? "loading" : undefined;
}

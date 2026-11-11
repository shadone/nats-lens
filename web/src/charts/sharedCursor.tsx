// One moment marked across the Overview's time charts and lanes. The chart
// under the pointer, or the focused one, owns the moment and shows its own
// readout; every other chart over the page's range draws a bare line at it.
// The line is moved by writing to the DOM from a subscription, so a pointer
// move re-renders only the chart under the pointer, never the followers.
import { createContext, useCallback, useContext, useEffect, useRef, type ReactNode, type RefObject } from "react";

export interface SharedCursorState {
  /** The moment, in Unix milliseconds; undefined when nothing is inspected. */
  time?: number;
  /** The chart or lane that set the moment. */
  owner?: string;
  /** Who made the latest change: the owner, a chart clearing its own, or "escape". */
  changedBy?: string;
}

export interface SharedCursorStore {
  get(): SharedCursorState;
  /** The page's range: only a chart drawn over it follows the moment. */
  page(): { start: number; end: number };
  /** Sets the moment, or with undefined clears it when `owner` still owns it. */
  set(owner: string, time: number | undefined): void;
  /** Clears the moment whoever owns it. */
  clear(by: string): void;
  subscribe(listener: (state: SharedCursorState, previous: SharedCursorState) => void): () => void;
}

export function createSharedCursor(page: () => { start: number; end: number }): SharedCursorStore {
  let state: SharedCursorState = {};
  const listeners = new Set<(s: SharedCursorState, p: SharedCursorState) => void>();
  const emit = (next: SharedCursorState) => {
    const previous = state;
    state = next;
    listeners.forEach((l) => l(state, previous));
  };
  return {
    get: () => state,
    page,
    set(owner, time) {
      if (time === undefined) {
        if (state.owner === owner) emit({ changedBy: owner });
        return;
      }
      if (state.owner === owner && state.time === time) return;
      emit({ time, owner, changedBy: owner });
    },
    clear(by) {
      if (state.owner !== undefined) emit({ changedBy: by });
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

const SharedCursorContext = createContext<SharedCursorStore | undefined>(undefined);

/** The shared cursor of one page over [start, end]. Escape anywhere clears it. */
export function SharedCursorProvider({ start, end, children }: { start: number; end: number; children: ReactNode }) {
  const range = useRef({ start, end });
  range.current = { start, end };
  const store = useRef<SharedCursorStore | undefined>(undefined);
  store.current ??= createSharedCursor(() => range.current);
  useEffect(() => {
    const escape = (e: KeyboardEvent) => {
      if (e.key === "Escape") store.current?.clear("escape");
    };
    document.addEventListener("keydown", escape);
    return () => document.removeEventListener("keydown", escape);
  }, []);
  return <SharedCursorContext.Provider value={store.current}>{children}</SharedCursorContext.Provider>;
}

export function useSharedCursorStore(): SharedCursorStore | undefined {
  return useContext(SharedCursorContext);
}

/**
 * Whether a chart over [start, end] shares the page's range: both ends
 * within a twentieth of the range's length, which lets the timeline's
 * whole buckets run a little past the range's end.
 */
export function sharesRange(start: number, end: number, page: { start: number; end: number }): boolean {
  const slack = Math.max(1, (page.end - page.start) / 20);
  return Math.abs(start - page.start) <= slack && Math.abs(end - page.end) <= slack;
}

/** Where a follower draws the moment, as a fraction of its width; undefined for no line. */
export function followerFraction(state: SharedCursorState, self: string, start: number, end: number, page: { start: number; end: number }, own = false): number | undefined {
  if (state.time === undefined || (!own && state.owner === self)) return undefined;
  if (end <= start || !sharesRange(start, end, page) || state.time < start || state.time > end) return undefined;
  return (state.time - start) / (end - start);
}

/**
 * Follows the shared moment for one chart over [start, end]: `place` gets
 * the line's fraction or undefined, straight from the store, without a
 * render. `onTaken` runs once when another chart, or Escape, takes the
 * moment from this one, so that only one readout shows. Returns the stable
 * setter of the moment this chart owns; undefined outside a provider.
 */
export function useSharedCursor(self: string, start: number, end: number, place: (fraction: number | undefined) => void, { own = false, onTaken }: { own?: boolean; onTaken?: () => void } = {}) {
  const store = useSharedCursorStore();
  const latest = useRef({ place, onTaken, start, end });
  latest.current = { place, onTaken, start, end };
  useEffect(() => {
    if (!store) return;
    const apply = (state: SharedCursorState, previous: SharedCursorState) => {
      const l = latest.current;
      l.place(followerFraction(state, self, l.start, l.end, store.page(), own));
      if (previous.owner === self && state.owner !== self && state.changedBy !== self) l.onTaken?.();
    };
    apply(store.get(), {});
    const unsubscribe = store.subscribe(apply);
    return () => {
      unsubscribe();
      store.set(self, undefined);
    };
  }, [store, self, own]);
  // A new frame places the line again without waiting for the next move.
  useEffect(() => {
    if (store) latest.current.place(followerFraction(store.get(), self, start, end, store.page(), own));
  }, [store, self, start, end, own]);
  const set = useCallback((time: number | undefined) => store?.set(self, time), [store, self]);
  return store ? set : undefined;
}

/** Shows or hides a follower's line element at a fraction of its box. */
export function placeLine(line: RefObject<HTMLElement | null>, fraction: number | undefined) {
  const el = line.current;
  if (!el) return;
  if (fraction === undefined) {
    el.removeAttribute("data-on");
    return;
  }
  el.style.left = `${(fraction * 100).toFixed(3)}%`;
  el.setAttribute("data-on", "");
}

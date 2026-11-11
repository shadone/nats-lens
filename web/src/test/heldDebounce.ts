// A useDebounced that a test can hold: while held, a value waits for the
// release, however long the gaps between a test's steps, and a value the
// hook gives at once (the first one, or one its reset asks for) still comes
// at once. A loaded runner can pause longer than a debounce between two
// keys; without the hold a test then sees a prefix settle. Mock the hook in
// a test file with
//
//   vi.mock("../lib/useDebounced", () => import("../test/heldDebounce"));
//
// useDebounced.test.ts covers the delay itself.
import { useSyncExternalStore } from "react";
import { vi } from "vitest";
import type * as Real from "../lib/useDebounced";

const actual = await vi.importActual<typeof Real>("../lib/useDebounced");
const listeners = new Set<() => void>();
let held = false;

const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
};

/** Holds or releases every mocked useDebounced; call it inside act. */
export function holdDebounce(on: boolean) {
  held = on;
  listeners.forEach((l) => l());
}

export function useDebounced<T extends string | number | undefined>(value: T, ms: number, now?: (settled: T, value: T) => boolean): T {
  const isHeld = useSyncExternalStore(subscribe, () => held);
  // The longest delay setTimeout takes.
  return actual.useDebounced(value, isHeld ? 2 ** 31 - 1 : ms, now);
}

import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useDebounced } from "./useDebounced";

afterEach(() => vi.useRealTimers());

describe("useDebounced", () => {
  it("gives the first set value at once and later ones after the delay", () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(({ v }) => useDebounced(v, 300), { initialProps: { v: "" } });
    expect(result.current).toBe("");
    rerender({ v: "a" });
    expect(result.current).toBe("a");
    rerender({ v: "b" });
    expect(result.current).toBe("a");
    rerender({ v: "c" });
    act(() => vi.advanceTimersByTime(299));
    expect(result.current).toBe("a");
    act(() => vi.advanceTimersByTime(1));
    expect(result.current).toBe("c");
  });
});

describe("useDebounced with a reset", () => {
  it("gives a value at once when the reset says so", () => {
    vi.useFakeTimers();
    const other = (s: string, v: string) => s.split("#")[0] !== v.split("#")[0];
    const { result, rerender } = renderHook(({ v }) => useDebounced(v, 300, other), { initialProps: { v: "prod#1" } });
    rerender({ v: "prod#2" });
    expect(result.current).toBe("prod#1");
    rerender({ v: "global#2" });
    expect(result.current).toBe("global#2");
  });
});

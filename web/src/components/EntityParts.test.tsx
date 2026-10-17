import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { usePager } from "./EntityParts";

describe("usePager", () => {
  it("walks forward and back through page tokens", () => {
    const { result } = renderHook(() => usePager("A"));
    act(() => result.current.next("t1"));
    act(() => result.current.next("t2"));
    expect(result.current).toMatchObject({ page: 3, token: "t2" });
    act(() => result.current.prev());
    expect(result.current).toMatchObject({ page: 2, token: "t1" });
  });

  it("starts again at page one on any change of filters, also back to earlier ones", () => {
    const { result, rerender } = renderHook(({ k }) => usePager(k), { initialProps: { k: "A" } });
    act(() => result.current.next("t1"));
    act(() => result.current.next("t2"));
    rerender({ k: "B" });
    expect(result.current).toMatchObject({ page: 1, token: "" });
    rerender({ k: "A" });
    expect(result.current).toMatchObject({ page: 1, token: "" });
  });
});

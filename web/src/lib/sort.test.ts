import { describe, expect, it } from "vitest";
import { readSort, sortRows } from "./sort";

describe("sortRows", () => {
  const rows = [
    { n: "b", v: 2 },
    { n: "x", v: undefined },
    { n: "a", v: 2 },
    { n: "c", v: 10 },
  ];
  it("keeps unset values last in either direction and ties in their order", () => {
    expect(sortRows(rows, (r) => r.v, "asc").map((r) => r.n)).toEqual(["b", "a", "c", "x"]);
    expect(sortRows(rows, (r) => r.v, "desc").map((r) => r.n)).toEqual(["c", "b", "a", "x"]);
  });
  it("sorts text by its letters", () => {
    expect(sortRows(rows, (r) => r.n, "asc").map((r) => r.n)).toEqual(["a", "b", "c", "x"]);
  });
});

describe("readSort", () => {
  it("reads a leading minus as descending", () => {
    expect(readSort("-bytes")).toEqual({ key: "bytes", dir: "desc" });
    expect(readSort("bucket")).toEqual({ key: "bucket", dir: "asc" });
    expect(readSort(null)).toBeUndefined();
  });
});

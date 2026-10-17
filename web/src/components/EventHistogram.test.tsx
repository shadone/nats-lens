import { describe, expect, it } from "vitest";
import { histogramMax } from "./EventHistogram";

const b = (warning: number, notable: number, routine: number) => ({ start: 0, warning, notable, routine });

describe("histogram scale", () => {
  it("follows warnings and notable events when routine ones are hidden", () => {
    expect(histogramMax([b(1, 2, 300), b(0, 5, 10)], true)).toBe(5);
  });
  it("follows every event when everything is shown", () => {
    expect(histogramMax([b(1, 2, 300), b(0, 5, 10)], false)).toBe(303);
  });
  it("is at least 1", () => {
    expect(histogramMax([b(0, 0, 9)], true)).toBe(1);
  });
});

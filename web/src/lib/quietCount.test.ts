import { describe, expect, it } from "vitest";
import { quietCount } from "./statusLine";

const tally = (muted: number, accepted?: number) => ({ critical: 0, warning: 0, info: 0, muted, flapping: 0, resolved24h: 0, accepted });

describe("quietCount", () => {
  it("counts accepted findings apart from muted ones", () => {
    expect(quietCount(tally(5, 3))).toBe("2 muted · 3 accepted");
    expect(quietCount(tally(2, 0))).toBe("2 muted");
    expect(quietCount(tally(3, 3))).toBe("3 accepted");
    expect(quietCount(tally(0))).toBe("");
  });
});

import { describe, expect, it } from "vitest";
import { fmtShortDuration } from "./format";

describe("fmtShortDuration", () => {
  it("keeps a round trip below 0.01 ms from reading as 0", () => {
    expect(fmtShortDuration(0.004)).toBe("<0.01 ms");
    expect(fmtShortDuration(0)).toBe("0 ms");
    expect(fmtShortDuration(0.42)).toBe("0.42 ms");
    expect(fmtShortDuration(4.06)).toBe("4.1 ms");
  });

  it("rolls a value that rounds up to the next unit", () => {
    expect(fmtShortDuration(999.6)).toBe("1 s");
    expect(fmtShortDuration(9.96)).toBe("10 ms");
    expect(fmtShortDuration(212.6)).toBe("213 ms");
    expect(fmtShortDuration(1500)).toBe("1.5 s");
  });
});

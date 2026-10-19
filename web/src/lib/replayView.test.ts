import { describe, expect, it } from "vitest";
import { stepNotes } from "./replayView";

const base = { stepsTruncated: true, stepsOmitted: 0 };

describe("stepNotes", () => {
  it("names the time the read limit was reached, with the day when A and B are on different days", () => {
    const sameDay = { ...base, a: Date.parse("2026-10-17T09:00:00Z"), b: Date.parse("2026-10-17T10:00:00Z"), completeUntil: Date.parse("2026-10-17T09:45:00Z") };
    expect(stepNotes(sameDay)).toEqual(["Steps after 09:45 may be missing: the read limit was reached."]);
    const twoDays = { ...base, a: Date.parse("2026-10-17T22:00:00Z"), b: Date.parse("2026-10-18T02:00:00Z"), completeUntil: Date.parse("2026-10-18T01:00:00Z") };
    expect(stepNotes(twoDays)).toEqual(["Steps after 18 Oct 01:00 may be missing: the read limit was reached."]);
  });
});

import { describe, expect, it } from "vitest";
import { collectionNotes, stepNotes } from "./replayView";

const base = { stepsTruncated: true, stepsOmitted: 0 };

describe("stepNotes", () => {
  it("names the time the read limit was reached, with the day when A and B are on different days", () => {
    const sameDay = { ...base, a: Date.parse("2026-10-17T09:00:00Z"), b: Date.parse("2026-10-17T10:00:00Z"), completeUntil: Date.parse("2026-10-17T09:45:00Z") };
    expect(stepNotes(sameDay)).toEqual(["Steps after 09:45 may be missing: the read limit was reached."]);
    const twoDays = { ...base, a: Date.parse("2026-10-17T22:00:00Z"), b: Date.parse("2026-10-18T02:00:00Z"), completeUntil: Date.parse("2026-10-18T01:00:00Z") };
    expect(stepNotes(twoDays)).toEqual(["Steps after 18 Oct 01:00 may be missing: the read limit was reached."]);
  });
});

describe("collectionNotes", () => {
  it("says when A is before the history starts", () => {
    const r = { a: Date.parse("2026-10-17T08:00:00Z"), historyStart: Date.parse("2026-10-17T08:30:00Z"), epochAAt: undefined, epochBAt: Date.parse("2026-10-17T09:00:00Z") };
    expect(collectionNotes(r)[0]).toBe("A is before the history starts, 17 Oct 08:30 UTC: nats-lens stored nothing before then.");
    expect(collectionNotes({ ...r, a: Date.parse("2026-10-17T08:45:00Z") })).toHaveLength(1);
  });
});

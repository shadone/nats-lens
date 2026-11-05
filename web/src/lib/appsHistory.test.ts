import { describe, expect, it } from "vitest";
import { appsHistoryText } from "./appsView";

const EPOCH = Date.parse("2026-11-05T22:47:18Z");
const NOW = EPOCH + 5_000;

describe("appsHistoryText", () => {
  it("says the hours before the history have no measurement when it begins inside the 24 hours", () => {
    expect(appsHistoryText(Date.parse("2026-11-05T13:47:18Z"), EPOCH, NOW)).toBe("The history begins at 13:47 UTC: the hours before it have no measurement.");
  });

  it("says nothing when the history covers the 24 whole hours before the collection's hour", () => {
    expect(appsHistoryText(Date.parse("2026-11-04T22:00:00Z"), EPOCH, NOW)).toBeUndefined();
    expect(appsHistoryText(Date.parse("2026-11-04T22:00:01Z"), EPOCH, NOW)).toBeDefined();
    expect(appsHistoryText(undefined, EPOCH, NOW)).toBeUndefined();
    expect(appsHistoryText(EPOCH, undefined, NOW)).toBeUndefined();
  });
});

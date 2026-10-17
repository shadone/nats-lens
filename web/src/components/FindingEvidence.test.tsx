import { describe, expect, it } from "vitest";
import { commonCreated } from "./FindingEvidence";

const day = 86_400_000;
const t0 = Date.UTC(2026, 9, 8, 9, 34);

describe("the creation time most copies share", () => {
  it("is the one most copies were created near, so the lone old copy stands out", () => {
    expect(commonCreated([t0, t0 + 65 * day, t0 + 65 * day + 60_000])).toBe(t0 + 65 * day);
  });
  it("is the earliest when no time is shared by more copies", () => {
    expect(commonCreated([t0 + 5 * day, t0])).toBe(t0);
  });
  it("is unset with no creation time", () => {
    expect(commonCreated([])).toBeUndefined();
  });
});

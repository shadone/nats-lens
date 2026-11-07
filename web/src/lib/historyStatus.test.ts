import { describe, expect, it } from "vitest";
import { readState } from "./historyStatus";

describe("readState", () => {
  it("says loading for a first read and nothing once it has an answer", () => {
    expect(readState({ error: null, isPending: true, errorUpdateCount: 0 })).toBe("loading");
    expect(readState({ error: null, isPending: false, errorUpdateCount: 0 })).toBeUndefined();
  });
  it("keeps a failed read failed while it retries without an answer", () => {
    expect(readState({ error: new Error("x"), isPending: false, errorUpdateCount: 1 })).toBe("failed");
    // A query without data that refetches drops its error and goes back to pending.
    expect(readState({ error: null, isPending: true, errorUpdateCount: 1 })).toBe("failed");
  });
  it("clears once a later read answers", () => {
    expect(readState({ error: null, isPending: false, errorUpdateCount: 1 })).toBeUndefined();
  });
});

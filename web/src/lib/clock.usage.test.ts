// Every "now" of the UI is serverNow(): a Date.now() elsewhere builds a
// range or a time on the browser's clock, which `natslens demo -fixed`
// refuses. The places that need the browser's own clock are listed here.
import { describe, expect, it } from "vitest";
import { uiSources } from "../test/sources";

/** Files and how many Date.now() or new Date() calls each may hold, with why. */
const ALLOWED: Record<string, number> = {
  // The clock itself: the browser's clock plus the server's offset.
  "./clock.ts": 2,
  // syncClock's receive time, which the offset is taken from.
  "../api/requests.ts": 1,
  // TanStack Query's own bookkeeping of when data arrived.
  "../api/events.ts": 1,
};

describe("the UI's now", () => {
  const sources = uiSources();

  it("reads the files it checks", () => {
    expect(Object.keys(sources).length).toBeGreaterThan(50);
    expect(Object.keys(sources)).toContain("../api/requests.ts");
  });

  it("calls Date.now() only where the browser's own clock is meant", () => {
    const found: Record<string, number> = {};
    for (const [path, text] of Object.entries(sources)) {
      const n = text.match(/\bDate\s*\.\s*now\s*\(|new Date\(\s*\)/g)?.length ?? 0;
      if (n > 0) found[path] = n;
    }
    expect(found).toEqual(ALLOWED);
  });
});

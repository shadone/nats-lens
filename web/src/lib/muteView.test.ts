import { describe, expect, it } from "vitest";
import { acceptance } from "../test/fixtures/capacity";
import { fmtGrowth } from "./format";
import { baselineText, endedByLens, muteActive, muteBy, reviewOn, sentence } from "./muteView";

describe("muteView", () => {
  it("names who accepted or muted", () => {
    expect(muteBy(acceptance())).toBe("Accepted by ops@example.com");
    expect(muteBy(acceptance({ accepted: false, mutedBy: "" }))).toBe("Muted by unknown");
  });

  it("gives an acceptance its review date and a mute none", () => {
    expect(reviewOn(acceptance())).toBe("Review on 2027-04-07.");
    expect(reviewOn(acceptance({ accepted: false }))).toBe("");
  });

  it("ends a reason with one full stop", () => {
    expect(sentence("Kept for life")).toBe("Kept for life.");
    expect(sentence("Kept for life. ")).toBe("Kept for life.");
  });

  it("says why nats-lens ended an acceptance, and nothing when a person lifted it", () => {
    const ended = acceptance({ unmutedAt: 1, unmutedBy: "nats-lens", endReason: "growth changed from +2.0 KiB/day to +520 KiB/day over the last 7 days (+1.6 MiB in the last day)" });
    expect(endedByLens(ended)).toBe("Ended by nats-lens: growth changed from +2.0 KiB/day to +520 KiB/day over the last 7 days (+1.6 MiB in the last day).");
    expect(endedByLens(acceptance({ unmutedAt: 1, unmutedBy: "ops" }))).toBe("");
  });

  it("is active until lifted or expired", () => {
    const m = acceptance();
    expect(muteActive(m, m.expiresAt! - 1)).toBe(true);
    expect(muteActive(m, m.expiresAt!)).toBe(false);
    expect(muteActive({ ...m, unmutedAt: 1 }, 0)).toBe(false);
  });

  it("says the recorded growth, or that it waits for 8 days of history", () => {
    expect(baselineText(acceptance(), fmtGrowth)).toMatch(/^Growth at acceptance: \+1 KiB\/day over the last 7 days;/);
    expect(baselineText(acceptance({ growthPerDay: undefined }), fmtGrowth)).toBe("No growth baseline is recorded yet: growth is recorded once the stream has 8 days of history.");
    expect(baselineText(acceptance({ accepted: false }), fmtGrowth)).toBe("");
    expect(baselineText(acceptance({ entity: undefined }), fmtGrowth)).toBe("");
  });
});

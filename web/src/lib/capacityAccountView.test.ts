import { describe, expect, it } from "vitest";
import { GIB, MIB, stream } from "../test/fixtures/capacity";
import { accountAsStore, boundText, fillsSoonText, growthLine, nextLimitText, storeUse, unboundedText } from "./capacityView";

const limit = 50 * GIB;

describe("account limits in the Capacity words", () => {
  it("names the account limit where a stream has no age, message or byte limit of its own", () => {
    expect(boundText(stream("s", { maxAge: undefined, accountLimit: limit }))).toBe("account limit 50 GiB (shared)");
    expect(boundText(stream("s", { maxAge: undefined, allowMsgTtl: true, accountLimit: limit }))).toBe("account limit 50 GiB (shared), per-message TTLs allowed");
    expect(boundText(stream("s", { maxAge: undefined, maxMsgsPerSubject: 100_000_000, accountLimit: limit }))).toBe("100M msgs per subject, account limit 50 GiB (shared)");
    expect(boundText(stream("OBJ_x", { kind: "object", maxAge: undefined, accountLimit: limit }))).toBe("account limit 50 GiB (shared)");
    expect(boundText(stream("KV_x", { kind: "kv", maxAge: undefined, accountLimit: limit }))).toBe("keys (no TTL), account limit 50 GiB (shared)");
  });

  it("keeps a stream's own limits as its bound, and says nothing of an account without a limit", () => {
    expect(boundText(stream("s", { maxBytes: 8 * GIB, accountLimit: limit }))).toBe("7 days, 8 GiB");
    expect(boundText(stream("s", { maxAge: undefined }))).toBe("nothing (keeps all)");
    expect(unboundedText(stream("s", { maxAge: undefined }))).toBe("unbounded");
    expect(unboundedText(stream("s", { maxAge: undefined, accountLimit: limit }))).toBe("account limit only");
  });

  it("words the account as the next limit by the account's time to full", () => {
    const next = (n: Partial<ReturnType<typeof stream>["next"]>) =>
      nextLimitText(stream("s", { maxAge: undefined, accountLimit: limit, next: { limit: "account", state: "reaches", discardNew: true, ...n } }));
    expect(next({ days: 9 })).toEqual({ text: "account limit 50 GiB (shared): account full in 9 days", crit: false });
    expect(next({ days: 4000 })).toEqual({ text: "account limit 50 GiB (shared): account full in more than 5 years", crit: false });
    expect(next({ state: "notReached" })).toEqual({ text: "account limit 50 GiB (shared): not reached", crit: false });
    expect(next({ state: "refusing" })).toEqual({ text: "account limit 50 GiB (shared): reached, refuses new messages", crit: true });
  });

  it("never gives a fills-soon tag the account's days", () => {
    const s = stream("s", { status: "fillsSoon", maxBytes: GIB, accountLimit: limit, next: { limit: "account", state: "reaches", days: 3, discardNew: true } });
    expect(fillsSoonText(s)).toBe("full soon");
  });

  it("reads an account card as a store", () => {
    const a = { account: "A", accountName: "APP", storage: "file" as const, used: 10 * GIB, limit, perDay: 512 * MIB, daysToFull: 80 };
    expect(storeUse(accountAsStore(a))).toEqual({ used: "10 GiB", of: "of 50 GiB · 20%", pct: 20 });
    expect(growthLine(accountAsStore(a))).toBe("+512 MiB/day · full in 3 months");
    expect(storeUse(accountAsStore({ ...a, used: undefined }))).toBeUndefined();
    expect(growthLine(accountAsStore({ ...a, perDay: undefined, daysToFull: undefined }))).toBe("not enough history for a rate (6 hours needed)");
  });
});

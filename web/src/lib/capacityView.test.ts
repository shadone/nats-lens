import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { DAY, EPOCH_AT, GIB, MIB, prod, store, stream } from "../test/fixtures/capacity";
import {
  ACCEPT_CHECK,
  boundText,
  clusterGroups,
  dataHeld,
  daysWords,
  fillsSoonText,
  fmtAge,
  fmtLimitCount,
  growthLine,
  headerLine,
  isBounded,
  kindLine,
  nextLimitText,
  perDayTimesR,
  sizeTimesR,
  storeFindingText,
  storeTone,
  storeUse,
  timeToFull,
} from "./capacityView";

describe("daysWords", () => {
  it("says hours, days, months and years, and more than 5 years beyond", () => {
    expect(daysWords(0.2)).toBe("5 hours");
    expect(daysWords(1 / 48)).toBe("1 hour");
    expect(daysWords(1)).toBe("1 day");
    expect(daysWords(2.75)).toBe("2.8 days");
    expect(daysWords(13.1)).toBe("13 days");
    expect(daysWords(59)).toBe("59 days");
    expect(daysWords(188)).toBe("6 months");
    expect(daysWords(1000)).toBe("3 years");
    expect(daysWords(5 * 365)).toBe("5 years");
    expect(daysWords(2895)).toBe("more than 5 years");
  });
});

describe("server cards", () => {
  it("shows the use of the limit and the share", () => {
    expect(storeUse(store("nats-0", { used: 244 * MIB, limit: 50 * GIB }))).toEqual({ used: "244 MiB", of: "of 50 GiB · 0.5%", pct: (244 / (50 * 1024)) * 100 });
    expect(storeUse(store("nats-0", { used: 71 * GIB, limit: 100 * GIB }))?.of).toBe("of 100 GiB · 71%");
  });

  it("shows the use alone without a limit, and nothing for a server not reported", () => {
    expect(storeUse(store("nats-0", { limit: undefined }))).toEqual({ used: "1.93 GiB", of: "no limit reported" });
    expect(storeUse(store("nats-0", { reported: false, used: undefined, limit: undefined }))).toBeUndefined();
  });

  it("words the time to full by the card's growth", () => {
    expect(timeToFull(store("a", { daysToFull: 9 }))).toBe("full in 9 days");
    expect(timeToFull(store("a", { daysToFull: 188 }))).toBe("full in 6 months");
    expect(timeToFull(store("a", { daysToFull: 1500 }))).toBe("full in 4 years");
    expect(timeToFull(store("a", { daysToFull: 2895 }))).toBe("full in more than 5 years at this rate");
    expect(timeToFull(store("a", { perDay: -MIB, daysToFull: undefined }))).toBe("not growing");
    expect(timeToFull(store("a", { perDay: 0, daysToFull: undefined }))).toBe("not growing");
    expect(timeToFull(store("a", { perDay: undefined, daysToFull: undefined }))).toBe("not enough history for a rate (6 hours needed)");
    expect(timeToFull(store("a", { limit: undefined, daysToFull: undefined }))).toBe("");
  });

  it("joins the growth and the time to full, and has no line for a server not reported", () => {
    expect(growthLine(store("a"))).toBe("+17 MiB/day · full in more than 5 years at this rate");
    expect(growthLine(store("a", { perDay: undefined, daysToFull: undefined }))).toBe("not enough history for a rate (6 hours needed)");
    expect(growthLine(store("a", { limit: undefined, daysToFull: undefined }))).toBe("+17 MiB/day");
    expect(growthLine(store("a", { reported: false }))).toBeUndefined();
  });

  it("names the finding with the check's own days, in its severity", () => {
    const s = store("nats-ap-2", { findingId: "server-014:nats-ap-2", findingCode: "server-014", findingSeverity: "warning", findingDays: 13.8 });
    expect(storeFindingText(s)).toBe("store full soon: 14 days by server-014");
    expect(storeTone(s)).toBe("warn");
    expect(storeTone({ ...s, findingSeverity: "critical" })).toBe("crit");
    expect(storeFindingText({ ...s, findingDays: undefined })).toBe("store full soon by server-014");
    expect(storeFindingText({ ...s, findingCode: "server-007" })).toBe("store usage high by server-007");
    expect(storeTone(store("a"))).toBeUndefined();
  });

  it("groups the cards by cluster in the server's order", () => {
    const g = clusterGroups([store("a", { cluster: "ap" }), store("b", { cluster: "ap" }), store("c", { cluster: "eu" })]);
    expect(g.map((x) => [x.cluster, x.servers.map((s) => s.name)])).toEqual([
      ["ap", ["a", "b"]],
      ["eu", ["c"]],
    ]);
  });
});

describe("limits in words", () => {
  it("writes an age limit in days from 3 days, else in hours or minutes", () => {
    expect(fmtAge(7 * DAY)).toBe("7 days");
    expect(fmtAge(DAY)).toBe("24 h");
    expect(fmtAge(2 * DAY)).toBe("48 h");
    expect(fmtAge(120_000)).toBe("2 min");
    expect(fmtAge(1_800_000)).toBe("30 min");
    expect(fmtAge(5_400_000)).toBe("1.5 h");
  });

  it("writes a message limit short when it is round", () => {
    expect(fmtLimitCount(1_000_000)).toBe("1M");
    expect(fmtLimitCount(100_000_000)).toBe("100M");
    expect(fmtLimitCount(100_000)).toBe("100k");
    expect(fmtLimitCount(96_500)).toBe("96,500");
  });
});

describe("kindLine", () => {
  it("names the replicas and the kind of each stream", () => {
    expect(kindLine(stream("order-jobs", { messages: 48_211 }))).toBe("R3 · limits · 48,211 msgs");
    expect(kindLine(stream("a", { retention: "interest", messages: 0 }))).toBe("R3 · interest");
    expect(kindLine(stream("a", { retention: "workqueue", messages: undefined }))).toBe("R3 · work queue");
    expect(kindLine(stream("a", { mirror: "order-events", messages: 0 }))).toBe("R3 · mirror of order-events");
    expect(kindLine(stream("a", { sources: 2, messages: 0 }))).toBe("R3 · sources 2 streams");
    expect(kindLine(stream("a", { sources: 1, messages: 0 }))).toBe("R3 · sources 1 stream");
    expect(kindLine(stream("KV_a", { kind: "kv", maxMsgsPerSubject: 1 }))).toBe("R3 · KV · 1 per key");
    expect(kindLine(stream("OBJ_a", { kind: "object" }))).toBe("R3 · object store");
    expect(kindLine(stream("KV_a", { kind: "kv", maxMsgsPerSubject: 1, storage: "memory", replicas: 1 }))).toBe("R1 · KV · 1 per key · memory");
  });
});

describe("boundText", () => {
  it("lists the limits that are set in the order age, messages, bytes, per subject", () => {
    expect(boundText(stream("a", { maxMsgs: 1_000_000 }))).toBe("7 days, 1M msgs");
    expect(boundText(stream("a", { maxAge: undefined, maxBytes: 8 * GIB }))).toBe("8 GiB");
    expect(boundText(stream("a", { maxAge: undefined, maxMsgsPerSubject: 100_000_000 }))).toBe("100M msgs per subject");
    expect(boundText(stream("a", { maxAge: undefined }))).toBe("nothing (keeps all)");
  });

  it("words a bucket by its TTL or its keys", () => {
    expect(boundText(stream("KV_a", { kind: "kv", maxAge: 2 * DAY }))).toBe("TTL 48 h");
    expect(boundText(stream("KV_a", { kind: "kv", maxAge: undefined }))).toBe("keys (no TTL)");
    expect(boundText(stream("KV_a", { kind: "kv", maxAge: undefined, allowMsgTtl: true }))).toBe("keys (no TTL), per-key TTLs allowed");
    expect(boundText(stream("KV_a", { kind: "kv", maxAge: undefined, maxBytes: 64 * MIB }))).toBe("64 MiB, no TTL");
    expect(boundText(stream("OBJ_a", { kind: "object", maxAge: 2 * DAY, maxBytes: 8 * GIB }))).toBe("TTL 48 h, 8 GiB");
    expect(boundText(stream("OBJ_a", { kind: "object", maxAge: undefined }))).toBe("nothing (keeps all)");
  });

  it("does not count a per-subject limit as a bound", () => {
    expect(isBounded(stream("a", { maxAge: undefined, maxMsgsPerSubject: 5 }))).toBe(false);
    expect(isBounded(stream("a", { maxAge: undefined, maxBytes: 1 }))).toBe(true);
  });
});

describe("dataHeld", () => {
  it("shows the age limit once the oldest message is at 95% of it", () => {
    expect(dataHeld(stream("a", { firstTs: EPOCH_AT - 6.7 * DAY }), EPOCH_AT).label).toBe("7 days");
    expect(dataHeld(stream("a", { firstTs: EPOCH_AT - 6.6 * DAY }), EPOCH_AT).label).toBe("since 2026-10-14");
  });

  it("spans the bar on a one-year scale, full beyond a year", () => {
    const h = dataHeld(stream("a", { maxAge: undefined, firstTs: EPOCH_AT - 73 * DAY }), EPOCH_AT);
    expect(h.pct).toBeCloseTo(20);
    expect(h.bounded).toBe(false);
    expect(dataHeld(stream("a", { firstTs: EPOCH_AT - 400 * DAY, maxAge: undefined }), EPOCH_AT).pct).toBe(100);
  });

  it("reads an empty stream as empty and a bucket by its keys", () => {
    expect(dataHeld(stream("a", { messages: 0, firstTs: undefined }), EPOCH_AT)).toEqual({ label: "empty", pct: 0, bounded: true });
    expect(dataHeld(stream("KV_a", { kind: "kv", keys: 214 }), EPOCH_AT).label).toBe("214 keys");
    expect(dataHeld(stream("KV_a", { kind: "kv", keys: 1 }), EPOCH_AT).label).toBe("1 key");
  });

  it("shows an en dash when the oldest message was not reported", () => {
    expect(dataHeld(stream("a", { messages: undefined, firstTs: undefined }), EPOCH_AT)).toEqual({ label: "–", pct: 0, bounded: true });
  });
});

describe("sizes", () => {
  it("counts every replica, and stays unset when a value is", () => {
    expect(sizeTimesR(stream("a", { bytes: 10, replicas: 3 }))).toBe(30);
    expect(perDayTimesR(stream("a", { bytesPerDay: -2, replicas: 3 }))).toBe(-6);
    expect(sizeTimesR(stream("a", { bytes: undefined }))).toBeUndefined();
    expect(perDayTimesR(stream("a", { bytesPerDay: undefined }))).toBeUndefined();
    expect(sizeTimesR(stream("a", { bytes: 0 }))).toBe(0);
  });
});

describe("nextLimitText", () => {
  const next = (o: Parameters<typeof stream>[1]) => nextLimitText(stream("a", o));
  it("words each state", () => {
    expect(next({ next: { limit: "none", state: undefined, discardNew: false } }).text).toBe("none: grows until the store is full");
    expect(next({ next: { limit: "empty", state: undefined, discardNew: false } }).text).toBe("empty");
    expect(next({ next: { limit: "age", state: "steady", discardNew: false }, maxAge: 30 * DAY }).text).toBe("steady at 30 days");
    expect(next({ next: { limit: "messages", state: "steady", discardNew: false }, maxMsgs: 1_000_000 }).text).toBe("steady at 1M msgs");
    expect(next({ next: { limit: "age", state: "reaches", days: 2.75, discardNew: false } }).text).toBe("7 days: in 2.8 days");
    expect(next({ next: { limit: "bytes", state: "reaches", days: 41, discardNew: true }, maxBytes: 8 * GIB }).text).toBe("8 GiB: in 41 days, then refuses new messages");
    expect(next({ next: { limit: "messages", state: "notReached", discardNew: false }, maxMsgs: 1_000_000 }).text).toBe("1M msgs: not reached");
    expect(next({ next: { limit: "perSubject", state: "notForecast", discardNew: false } }).text).toBe("per subject only: not forecast");
    expect(next({ next: { limit: "unknown", state: undefined, discardNew: false } }).text).toBe("–");
  });

  it("marks a stream that refuses messages now as critical, in words too", () => {
    const n = next({ next: { limit: "messages", state: "refusing", discardNew: true }, maxMsgs: 96_500 });
    expect(n).toEqual({ text: "96,500 msgs: reached, refuses new messages", crit: true });
  });
});

describe("fillsSoonText", () => {
  it("uses the check's own days, else the page's forecast", () => {
    expect(fillsSoonText(stream("a", { findingDays: 9 }))).toBe("full in 9 days");
    expect(fillsSoonText(stream("a", { next: { limit: "bytes", state: "reaches", days: 12, discardNew: true } }))).toBe("full in 12 days");
    expect(fillsSoonText(stream("a"))).toBe("full soon");
  });
});

describe("ACCEPT_CHECK", () => {
  it("names stream-005 as its check file does", () => {
    // Through a function: vite rewrites a literal new URL(..., import.meta.url) into a served URL.
    const read = (f: string) => readFileSync(new URL(f, import.meta.url), "utf8");
    const sql = read("../../../checks/stream-005.sql");
    expect(sql).toContain(`code: ${ACCEPT_CHECK.code}\nname: ${ACCEPT_CHECK.name}\n`);
  });
});

describe("headerLine", () => {
  it("says since when growth is read, or that no rate exists yet", () => {
    const d = prod();
    expect(headerLine(d)).toBe("9 resources · 3 stores · growth from 2026-10-20 12:00 UTC");
    const young = { ...d, servers: d.servers.map((s) => ({ ...s, perDay: undefined, daysToFull: undefined })), streams: d.streams.map((s) => ({ ...s, bytesPerDay: undefined })) };
    expect(headerLine(young)).toBe("9 resources · 3 stores · no growth rate yet (6 hours of history needed)");
  });
});

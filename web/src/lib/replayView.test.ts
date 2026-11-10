import { describe, expect, it } from "vitest";
import { collectionNotes, stepNotes, unobservedText, whenFormat } from "./replayView";

const base = { stepsTruncated: true, stepsOmitted: 0 };

describe("stepNotes", () => {
  it("names the time the read limit was reached, with the day when A and B are on different days", () => {
    const sameDay = { ...base, a: Date.parse("2026-10-17T09:00:00Z"), b: Date.parse("2026-10-17T10:00:00Z"), completeUntil: Date.parse("2026-10-17T09:45:00Z") };
    expect(stepNotes(sameDay)).toEqual(["Steps after 09:45 may be missing: the read limit was reached."]);
    const twoDays = { ...base, a: Date.parse("2026-10-17T22:00:00Z"), b: Date.parse("2026-10-18T02:00:00Z"), completeUntil: Date.parse("2026-10-18T01:00:00Z") };
    expect(stepNotes(twoDays)).toEqual(["Steps after 18 Oct 01:00 may be missing: the read limit was reached."]);
  });

  it("says that catch-ups may be missing beside the other notes", () => {
    const r = { ...base, a: Date.parse("2026-10-17T09:00:00Z"), b: Date.parse("2026-10-17T10:00:00Z"), completeUntil: Date.parse("2026-10-17T09:45:00Z"), catchUpsTruncated: true };
    expect(stepNotes(r)).toEqual([
      "Steps after 09:45 may be missing: the read limit was reached.",
      "Replicas catching up may be missing: the read limit was reached.",
    ]);
    expect(stepNotes({ ...r, completeUntil: undefined })).toEqual(["Replicas catching up may be missing: the read limit was reached."]);
  });
});

describe("collectionNotes", () => {
  it("says when A is before the history starts", () => {
    const r = { a: Date.parse("2026-10-17T08:00:00Z"), historyStart: Date.parse("2026-10-17T08:30:00Z"), epochAAt: undefined, epochBAt: Date.parse("2026-10-17T09:00:00Z") };
    expect(collectionNotes(r)[0]).toBe("A is before the history starts, 17 Oct 08:30 UTC: nats-lens stored nothing before then.");
    expect(collectionNotes({ ...r, a: Date.parse("2026-10-17T08:45:00Z") })).toHaveLength(1);
  });
});

describe("times on another day than A", () => {
  const m = { a: Date.parse("2026-10-17T09:00:00Z"), b: Date.parse("2026-10-17T10:00:00Z") };

  it("carry the day, though A and B fall on one day", () => {
    const fmt = whenFormat(m);
    expect(fmt(Date.parse("2026-10-17T09:30:00Z"))).toBe("09:30:00");
    expect(fmt(Date.parse("2026-10-14T23:00:00Z"))).toBe("14 Oct 23:00:00");
    const u = { count: "leadersMoved", reason: "collector_gap" as const, from: Date.parse("2026-10-14T23:00:00Z"), to: Date.parse("2026-10-17T09:10:00Z") };
    expect(unobservedText(u, fmt)).toBe("nats-lens was not running 14 Oct 23:00:00–09:10:00");
  });

  it("carry the day in the read-limit note", () => {
    expect(stepNotes({ ...m, stepsTruncated: true, stepsOmitted: 0, completeUntil: Date.parse("2026-10-18T01:00:00Z") })).toEqual(["Steps after 18 Oct 01:00 may be missing: the read limit was reached."]);
  });
});

describe("a lost events connection", () => {
  const fmt = whenFormat({ a: Date.parse("2026-10-17T09:00:00Z"), b: Date.parse("2026-10-17T10:00:00Z") });

  it("says the events were not captured, with the connection's lost time", () => {
    const at = (h: string) => Date.parse(`2026-10-17T${h}Z`);
    expect(unobservedText({ count: "leadersMoved", reason: "events_connection_lost", from: at("09:40:00"), to: at("09:52:00") }, fmt)).toBe(
      "no events captured: an events connection was lost 09:40:00–09:52:00",
    );
    expect(unobservedText({ count: "clientReconnects", reason: "events_connection_lost", from: at("09:40:00") }, fmt)).toBe(
      "no events captured: an events connection was lost 09:40:00–still at B",
    );
  });
});

describe("client-library collection evidence", () => {
  it("opens exact application and account at the observed time with encoded identity", async () => {
    const { evidencePath } = await import("./replayView");
    const to = evidencePath({ kind: "collection", id: "7", time: Date.parse("2026-10-17T09:00:00Z"), label: "A", entity: { kind: "client", key: "shop & /", name: "shop", account: "A&B", stream: "", consumer: "", server: "" } });
    const u = new URL(to!, "https://example.com");
    expect(u.pathname).toBe("/applications");
    expect(u.searchParams.get("app")).toBe("shop & /");
    expect(u.searchParams.get("account")).toBe("A&B");
    expect(u.searchParams.get("to")).toBe("2026-10-17T09:00:01Z");
  });
});

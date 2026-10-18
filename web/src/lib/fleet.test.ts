import { describe, expect, it } from "vitest";
import type { Finding, SystemBadge, SystemSummary } from "../api/model";
import { cardState, fleetBadge, freshnessText, headline, notCollecting, worstFirst } from "./fleet";

const NOW = Date.parse("2026-10-17T17:00:00Z");

function sys(id: string, over: Partial<SystemSummary> = {}): SystemSummary {
  return {
    id,
    name: id,
    environment: "",
    collection: "collecting",
    collectionError: "",
    status: "ok",
    tally: { critical: 0, warning: 0, info: 0, muted: 0, flapping: 0, resolved24h: 0 },
    strip: [],
    error: "",
    ...over,
  };
}

const finding = (checkName: string, name: string): Finding =>
  ({
    id: "1",
    checkCode: "consumer-010",
    checkName,
    severity: "critical",
    state: "firing",
    entity: { kind: "consumer", key: "", name, account: "APP", stream: "order-jobs", consumer: name, server: "" },
    detail: "",
    remediation: "",
    relatedConfig: [],
    relatedConfigSource: "",
    transitionsLastHour: 0,
  }) as Finding;

describe("cardState", () => {
  it("is not collecting when the connection is closed, whatever the findings say", () => {
    expect(cardState(sys("a", { collection: "connection closed", status: "critical" }))).toBe("notcollecting");
    expect(cardState(sys("a", { collection: "not connected" }))).toBe("notcollecting");
  });

  it("is unavailable when the summary has an error", () => {
    expect(cardState(sys("a", { error: "deadline exceeded", status: "nodata" }))).toBe("unavailable");
  });

  it("reads a system with only info findings as ok", () => {
    expect(cardState(sys("a", { status: "info", tally: { critical: 0, warning: 0, info: 2, muted: 0, flapping: 0, resolved24h: 0 } }))).toBe("ok");
  });

  it("follows the status otherwise; the demo counts as collecting", () => {
    expect(cardState(sys("a", { status: "warning", collection: "demo" }))).toBe("warning");
    expect(cardState(sys("a", { status: "nodata", collection: "starting" }))).toBe("nodata");
  });
});

describe("worstFirst", () => {
  it("orders critical, warning, not collecting, unavailable, no data, muted, then ok with info inside ok, keeping configuration order in a tie", () => {
    const list = [
      sys("ok-1"),
      sys("closed", { collection: "connection closed", status: "info" }),
      sys("info", { status: "info" }),
      sys("muted", { status: "muted" }),
      sys("ok-2"),
      sys("nodata", { status: "nodata" }),
      sys("gone", { error: "deadline exceeded" }),
      sys("warn", { status: "warning" }),
      sys("crit", { status: "critical" }),
    ];
    expect(worstFirst(list).map((s) => s.id)).toEqual(["crit", "warn", "closed", "gone", "nodata", "muted", "ok-1", "info", "ok-2"]);
  });

  it("does not reorder the list it is given", () => {
    const list = [sys("ok"), sys("crit", { status: "critical" })];
    worstFirst(list);
    expect(list.map((s) => s.id)).toEqual(["ok", "crit"]);
  });
});

describe("headline", () => {
  it("names the info finding of an ok system, as a notice", () => {
    const tally = { critical: 0, warning: 0, info: 2, muted: 0, flapping: 0, resolved24h: 0 };
    const h = headline(sys("a", { status: "info", tally, worst: finding("Stream without any limit", "catalog-changes") }), NOW);
    expect(h).toEqual({ text: "Stream without any limit: catalog-changes", more: 1 });
    expect(headline(sys("a"), NOW).text).toBe("No findings firing");
  });

  it("names the worst finding and its entity", () => {
    expect(headline(sys("a", { status: "critical", worst: finding("Expected consumer missing", "analytics-aggregator") }), NOW).text).toBe(
      "Expected consumer missing: analytics-aggregator",
    );
  });

  it("gives the collection error with since when not collecting", () => {
    const h = headline(sys("a", { collection: "connection closed", collectionError: "nats: authorization violation", collectionErrorSince: NOW - 3 * 3_600_000 }), NOW);
    expect(h.text).toBe("nats: authorization violation since 14:00 UTC");
  });

  it("gives the date with since for an error older than a day", () => {
    const h = headline(sys("a", { collection: "not connected", collectionErrorSince: Date.parse("2026-10-14T08:05:00Z") }), NOW);
    expect(h.text).toBe("Not connected since 14 Oct 08:05 UTC");
  });
});

describe("freshnessText", () => {
  const fresh = (ageMs: number, live = true) => ({ epoch: "9", epochAt: NOW - ageMs, partial: false, live, intervalMs: 30_000, collecting: true });

  it("says live with the age of the newest epoch", () => {
    expect(freshnessText(sys("a", { freshness: fresh(6_000) }), true, NOW)).toBe("live · 6s");
  });

  it("says when data last arrived once it is older than three intervals or the system is not collecting", () => {
    expect(freshnessText(sys("a", { freshness: fresh(3 * 3_600_000) }), true, NOW)).toBe("last data 3h ago");
    expect(freshnessText(sys("a", { freshness: fresh(6_000), collection: "connection closed" }), true, NOW)).toBe("last data 6s ago");
  });

  it("says no data without an epoch, and gives the newest epoch's time in a fixed range", () => {
    expect(freshnessText(sys("a"), true, NOW)).toBe("no data");
    expect(freshnessText(sys("a", { freshness: fresh(3_600_000) }), false, NOW)).toBe("newest 17 Oct 16:00");
  });
});

describe("fleetBadge", () => {
  // The badge reads each system's badge, which the server derives from its summary as notCollecting reads it.
  const fb = (xs?: SystemSummary[]) =>
    fleetBadge(xs?.map((s): SystemBadge => ({ id: s.id, name: s.name, environment: s.environment, status: s.status, collection: s.collection, collecting: !notCollecting(s), error: s.error })));

  it("counts the systems that are not OK, at the level of the worst", () => {
    const b = fb([sys("a"), sys("b", { status: "warning" }), sys("c", { collection: "connection closed" }), sys("d", { status: "muted" })]);
    expect(b).toEqual({ count: 2, level: "warning", what: "not OK", worst: "warning" });
  });

  it("names a worst state that is not a severity in words", () => {
    expect(fb([sys("c", { collection: "connection closed" })])).toEqual({ count: 1, level: "nodata", what: "not OK", worst: "not collecting" });
  });

  it("does not count info findings, and counts no data", () => {
    expect(fb([sys("a", { status: "info" })])).toBeUndefined();
    expect(fb([sys("a", { status: "info" }), sys("b", { status: "nodata" })])).toEqual({ count: 1, level: "nodata", what: "not OK", worst: "no data" });
  });

  it("counts a system whose status failed or that does not collect", () => {
    const b = (over: Partial<SystemBadge>): SystemBadge => ({ id: "a", name: "a", environment: "", status: "ok", collection: "collecting", collecting: true, error: "", ...over });
    expect(fleetBadge([b({ error: "the summary took longer than 5s" })])).toMatchObject({ count: 1, worst: "summary unavailable" });
    expect(fleetBadge([b({ collecting: false, collection: "demo" })])).toMatchObject({ count: 1, worst: "not collecting" });
  });

  it("has no badge when every system is OK, or before the summaries arrive", () => {
    expect(fb([sys("a"), sys("b", { status: "muted" })])).toBeUndefined();
    expect(fb(undefined)).toBeUndefined();
  });
});

describe("the freshness's collecting flag", () => {
  const stopped = { epoch: "9", epochAt: NOW - 3 * 3_600_000, partial: false, live: true, intervalMs: 30_000, collecting: false, notCollectingSince: Date.parse("2026-10-17T14:00:00Z") };

  it("makes a card not collecting, since the time the freshness gives", () => {
    const s = sys("a", { freshness: stopped });
    expect(cardState(s)).toBe("notcollecting");
    expect(headline(s, NOW).text).toBe("Not collecting since 14:00 UTC");
    expect(freshnessText(s, true, NOW)).toBe("last data 3h ago");
  });
});

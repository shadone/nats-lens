import { describe, expect, it } from "vitest";
import type { Finding, SystemSummary } from "../api/model";
import { cardState, fleetBadge, freshnessText, headline, worstFirst } from "./fleet";

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
    checkCode: "consumer-006",
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

  it("follows the status otherwise; the demo counts as collecting", () => {
    expect(cardState(sys("a", { status: "warning", collection: "demo" }))).toBe("warning");
    expect(cardState(sys("a", { status: "nodata", collection: "starting" }))).toBe("nodata");
  });
});

describe("worstFirst", () => {
  it("orders critical, warning, info, not collecting, then ok, keeping configuration order in a tie", () => {
    const list = [
      sys("ok-1"),
      sys("closed", { collection: "connection closed", status: "info" }),
      sys("info", { status: "info" }),
      sys("ok-2"),
      sys("warn", { status: "warning" }),
      sys("crit", { status: "critical" }),
    ];
    expect(worstFirst(list).map((s) => s.id)).toEqual(["crit", "warn", "info", "closed", "ok-1", "ok-2"]);
  });

  it("does not reorder the list it is given", () => {
    const list = [sys("ok"), sys("crit", { status: "critical" })];
    worstFirst(list);
    expect(list.map((s) => s.id)).toEqual(["ok", "crit"]);
  });
});

describe("headline", () => {
  it("names the worst finding and its entity", () => {
    expect(headline(sys("a", { status: "critical", worst: finding("Expected durable missing", "analytics-aggregator") }), NOW).text).toBe(
      "Expected durable missing: analytics-aggregator",
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
  it("counts the systems that are not OK, at the level of the worst", () => {
    const b = fleetBadge([sys("a"), sys("b", { status: "warning" }), sys("c", { collection: "connection closed" }), sys("d", { status: "muted" })]);
    expect(b).toEqual({ count: 2, level: "warning", what: "not OK", worst: "warning" });
  });

  it("names a worst state that is not a severity in words", () => {
    expect(fleetBadge([sys("c", { collection: "connection closed" })])).toEqual({ count: 1, level: "nodata", what: "not OK", worst: "not collecting" });
  });

  it("has no badge when every system is OK, or before the summaries arrive", () => {
    expect(fleetBadge([sys("a"), sys("b", { status: "muted" })])).toBeUndefined();
    expect(fleetBadge(undefined)).toBeUndefined();
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

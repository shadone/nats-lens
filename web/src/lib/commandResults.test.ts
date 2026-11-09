import { describe, expect, it } from "vitest";
import type { Finding, LensEvent } from "../api/model";
import type { SearchItem } from "../api/search";
import { parseCommand } from "./commandQuery";
import { eventRow, findingRow, GROUP_CAP, groupResults, moreLabel, NARROWED_CAP, REMOTE_CAP, type RemoteResult } from "./commandResults";

const NOW = Date.UTC(2026, 9, 17, 14, 30);
const item = (kind: SearchItem["kind"], name: string): SearchItem => ({ kind, name, sub: "", path: `/${kind}/${name}` });
const idle: RemoteResult<never> = { state: "idle", items: [], total: 0 };

const finding = (over: Partial<Finding> = {}): Finding =>
  ({
    id: "consumer-010:APP/order-archive/analytics-aggregator",
    checkCode: "consumer-010",
    checkName: "Expected durable missing",
    severity: "warning",
    state: "firing",
    entity: { kind: "consumer", key: "APP/order-archive/analytics-aggregator", name: "analytics-aggregator", account: "APP", stream: "order-archive", consumer: "analytics-aggregator", server: "" },
    since: Date.UTC(2026, 9, 17, 9, 31),
    ...over,
  }) as Finding;

const event = (over: Partial<LensEvent> = {}): LensEvent =>
  ({ id: "42", time: Date.UTC(2026, 9, 17, 9, 31, 7), type: "CONSUMER.DELETED", kind: "consumer.deleted", summary: "s", consumer: "analytics-aggregator", stream: "order-archive", server: "", clientName: "", ...over }) as LensEvent;

describe("groupResults", () => {
  const names = [
    ...Array.from({ length: 6 }, (_, i) => item("stream", `orders-${i}`)),
    item("consumer", "order-worker"),
    item("server", "nats-1"),
  ];

  it("groups names by kind in a fixed order, each capped, with a row to narrow", () => {
    const groups = groupResults(parseCommand("order"), names, idle, idle, NOW);
    expect(groups.map((g) => [g.label, g.rows.length, g.total])).toEqual([
      ["Streams", GROUP_CAP, 6],
      ["Consumers", 1, 1],
    ]);
    expect(groups[0].more).toEqual({ kind: "narrow", to: "stream" });
    expect(moreLabel(groups[0])).toBe("Show all 6");
    expect(groups[1].more).toBeUndefined();
  });

  it("shows one kind with a larger cap when narrowed, and offers its page", () => {
    const many = Array.from({ length: 25 }, (_, i) => item("stream", `s-${String(i).padStart(2, "0")}`));
    const groups = groupResults(parseCommand("stream:s-"), [...many, item("consumer", "s-worker")], idle, idle, NOW);
    expect(groups).toHaveLength(1);
    expect(groups[0].rows).toHaveLength(NARROWED_CAP);
    expect(groups[0].more).toEqual({ kind: "page", path: "/streams" });
    expect(moreLabel(groups[0])).toBe("Open the streams page");
    const kv = groupResults(parseCommand("kv:"), Array.from({ length: 21 }, (_, i) => item("kv", `b${i}`)), idle, idle, NOW);
    expect(moreLabel(kv[0])).toBe("Open the KV stores page");
  });

  it("lists a kind by name when its prefix comes with no text", () => {
    const groups = groupResults(parseCommand("server:"), [item("server", "nats-2"), item("server", "nats-1"), item("stream", "x")], idle, idle, NOW);
    expect(groups.map((g) => g.rows.map((r) => r.name))).toEqual([["nats-1", "nats-2"]]);
  });

  it("puts findings and events after the names, with their state while they load or fail", () => {
    const loading: RemoteResult<Finding> = { state: "loading", items: [], total: 0 };
    const failed: RemoteResult<LensEvent> = { state: "error", items: [], total: 0 };
    const groups = groupResults(parseCommand("order"), names, loading, failed, NOW);
    expect(groups.map((g) => [g.label, g.state])).toEqual([
      ["Streams", "ready"],
      ["Consumers", "ready"],
      ["Findings", "loading"],
      ["Events", "error"],
    ]);
  });

  it("leaves out an empty findings or events group, and caps the others", () => {
    const fs = Array.from({ length: 7 }, (_, i) => finding({ id: `f${i}` }));
    const groups = groupResults(parseCommand("zz"), [], { state: "ready", items: fs, total: 12 }, { state: "ready", items: [], total: 0 }, NOW);
    expect(groups.map((g) => [g.label, g.rows.length])).toEqual([["Findings", REMOTE_CAP]]);
    expect(moreLabel(groups[0])).toBe("Show all 12");
  });
});

describe("rows", () => {
  it("names a finding by its code and entity, with severity and since", () => {
    expect(findingRow(finding(), NOW)).toMatchObject({
      name: "consumer-010",
      detail: "analytics-aggregator",
      note: "Warning · firing since 09:31",
      path: "/findings/consumer-010%3AAPP%2Forder-archive%2Fanalytics-aggregator",
    });
    expect(findingRow(finding({ mute: { accepted: false } as Finding["mute"], since: Date.UTC(2026, 9, 15, 8, 0) }), NOW).note).toBe(
      "Warning · muted · firing since 15 Oct 08:00",
    );
  });

  it("names an event by its subject and type, with its time, and opens it on the events page", () => {
    expect(eventRow(event(), NOW)).toMatchObject({ name: "analytics-aggregator", detail: "CONSUMER.DELETED", note: "09:31", path: "/events?id=42" });
    expect(eventRow(event({ consumer: "", stream: "", summary: "collector reconnected" }), NOW)).toMatchObject({
      name: "CONSUMER.DELETED",
      detail: "collector reconnected",
    });
  });
});

import { describe, expect, it } from "vitest";
import type { Matrix, MatrixAsset } from "../api/placementMatrix";
import { masset, mcell } from "../test/fixtures/placement";
import { cellText, cellTone, changes, compareAt, infoCommand, meaning, peersAgree, stepDownCommand } from "./placementView";

const T = Date.parse("2026-10-17T14:30:00Z");

describe("cellText", () => {
  const stream = masset("stream", "order-jobs", "", []);
  const consumer = masset("consumer", "order-jobs", "resize-worker", []);
  it("says what differs first, then the state, then the lag", () => {
    expect(cellText(consumer, mcell("nats-0", "in_sync", { differs: ["created"], created: Date.parse("2026-08-12T10:02:11Z"), lag: 0 }))).toBe("created 08-12 10:02");
    expect(cellText(stream, mcell("nats-1", "disagrees", { differs: ["config_hash"] }))).toBe("config differs");
    expect(cellText(stream, mcell("nats-1", "in_sync", { lag: 0 }))).toBe("lag 0");
    expect(cellText(stream, mcell("nats-1", "behind", { lag: 420 }))).toBe("lag 420");
    expect(cellText(stream, mcell("nats-0", "leader", { lastSeq: 7378 }))).toBe("last seq 7,378");
    expect(cellText(consumer, mcell("nats-0", "leader", { delivered: 12 }))).toBe("delivered 12");
    expect(cellText(stream, mcell("nats-2", "missing"))).toBe("reports nothing");
    expect(cellText(stream, mcell("nats-2", "offline"))).toBe("offline");
    expect(cellText(stream, mcell("nats-2", "unknown"))).toBe("no leader");
  });
  it("tones a disagreement whatever the state", () => {
    expect(cellTone(mcell("a", "in_sync", { differs: ["created"] }))).toBe("disagree");
    expect(cellTone(mcell("a", "leader"))).toBe("ok");
    expect(cellTone(mcell("a", "behind"))).toBe("behind");
    expect(cellTone(undefined)).toBe("none");
  });
});

describe("commands", () => {
  it("names the stream and consumer, quoted when needed, and asks jq for the creation time when it differs", () => {
    const c = masset("consumer", "order-jobs", "resize-worker", []);
    expect(infoCommand(c)).toBe("nats consumer info order-jobs resize-worker --json");
    expect(infoCommand(c, mcell("nats-0", "in_sync", { differs: ["created"] }))).toBe("nats consumer info order-jobs resize-worker --json | jq .created");
    expect(infoCommand(masset("stream", "odd name", "", []))).toBe("nats stream info 'odd name' --json");
    expect(stepDownCommand(masset("stream", "order-jobs", "", []))).toBe("nats stream cluster step-down order-jobs");
    expect(stepDownCommand(c)).toBe("nats consumer cluster step-down order-jobs resize-worker");
  });
});

describe("peers and meaning", () => {
  it("agrees when every server that names peers names the same set", () => {
    const a = masset("stream", "s", "", [mcell("n0", "leader", { peers: ["n1", "n2"] }), mcell("n1", "in_sync", { peers: ["n0", "n2"] }), mcell("n2", "missing")]);
    expect(peersAgree(a)).toBe(true);
    a.cells[1].peers = ["n0", "n3"];
    expect(peersAgree(a)).toBe(false);
  });
  it("explains a creation time apart and a missing replica", () => {
    const a = masset("consumer", "s", "c", []);
    expect(meaning(a, mcell("n0", "in_sync", { differs: ["created"] }))).toMatch(/another creation time for the consumer/);
    expect(meaning(a, mcell("n2", "missing"))).toMatch(/reports no consumer/);
  });
});

describe("changes", () => {
  const m = (streams: MatrixAsset[], servers = ["n0", "n1", "n2"]): Matrix => ({
    servers: servers.map((name) => ({ name, metaLeader: false, present: true })),
    streams,
    counts: { all: 0, disagree: 0, behind: 0, missing: 0, single: 0 },
  });
  it("marks a state that differs, a replica that moved and an asset that is new", () => {
    const now = m([
      masset("stream", "s", "", [mcell("n0", "leader"), mcell("n1", "offline"), mcell("n2", "in_sync")], {
        consumers: [masset("consumer", "s", "new", [mcell("n0", "leader")])],
      }),
    ]);
    const then = m([masset("stream", "s", "", [mcell("n0", "leader"), mcell("n1", "in_sync"), mcell("n3", "in_sync")])], ["n0", "n1", "n3"]);
    const got = changes(now, then);
    expect([...got.keys()].sort()).toEqual(["APP/s/new|n0", "APP/s|n1", "APP/s|n2", "APP/s|n3"]);
    expect(got.get("APP/s|n1")).toEqual({ then: "in_sync", newAsset: false });
    expect(got.get("APP/s|n2")?.then).toBe("not_placed");
    expect(got.get("APP/s|n3")?.then).toBe("in_sync");
    expect(got.get("APP/s/new|n0")).toEqual({ then: "not_placed", newAsset: true });
  });
  it("reads this time yesterday and a UTC time from the URL", () => {
    expect(compareAt("1d", T)).toBe(T - 86_400_000);
    expect(compareAt("2026-10-16T09:00:00Z", T)).toBe(Date.parse("2026-10-16T09:00:00Z"));
    expect(compareAt("", T)).toBeUndefined();
    expect(compareAt("yesterday-ish", T)).toBeUndefined();
  });
});

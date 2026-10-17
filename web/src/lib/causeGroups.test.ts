import { describe, expect, it } from "vitest";
import type { Entity, Finding } from "../api/model";
import { groupCauses, parentKey } from "./causeGroups";

function consumerEnt(stream: string, name: string): Entity {
  return { kind: "consumer", key: `APP/${stream}/${name}`, name, account: "APP", stream, consumer: name, server: "" };
}

function streamEnt(stream: string): Entity {
  return { kind: "stream", key: `APP/${stream}`, name: stream, account: "APP", stream, consumer: "", server: "" };
}

function f(id: string, code: string, e: Entity | undefined, extra: Partial<Finding> = {}): Finding {
  return {
    id,
    checkCode: code,
    checkName: `name of ${code}`,
    severity: "info",
    state: "firing",
    entity: e,
    detail: `detail ${id}`,
    remediation: "",
    relatedConfig: [],
    relatedConfigSource: "",
    transitionsLastHour: 0,
    ...extra,
  };
}

describe("groupCauses", () => {
  it("makes one group of four consumer-009 findings on one stream", () => {
    const fs = ["resize-worker", "thumb-worker", "job-cache-projector", "audit-writer"].map((n) => f(`c9:${n}`, "consumer-009", consumerEnt("order-jobs", n)));
    const { groups, muted } = groupCauses(fs);
    expect(groups).toHaveLength(1);
    expect(groups[0].findings).toHaveLength(4);
    expect(groups[0].checkCodes).toEqual(["consumer-009"]);
    expect(groups[0].parent).toEqual({ kind: "stream", name: "order-jobs" });
    expect(muted).toEqual([]);
  });

  it("keeps consumers of two streams apart", () => {
    const { groups } = groupCauses([f("a", "consumer-009", consumerEnt("order-jobs", "a")), f("b", "consumer-009", consumerEnt("media-windows", "b"))]);
    expect(groups).toHaveLength(2);
  });

  it("groups findings of different checks that name one cause", () => {
    const cause = { id: "ev-1", kind: "consumer.deleted", summary: "x" };
    const { groups } = groupCauses([
      f("a", "consumer-006", consumerEnt("order-archive", "a"), { cause }),
      f("b", "stream-013", streamEnt("order-archive"), { cause }),
    ]);
    expect(groups).toHaveLength(1);
    expect(groups[0].cause?.id).toBe("ev-1");
    expect(groups[0].checkCodes).toEqual(["consumer-006", "stream-013"]);
  });

  it("puts the worst severity first, and muted findings in their own list", () => {
    const { groups, muted } = groupCauses([
      f("i", "stream-025", streamEnt("a"), { severity: "info" }),
      f("c", "consumer-006", consumerEnt("b", "x"), { severity: "critical" }),
      f("m", "stream-031", streamEnt("c"), { severity: "warning", mute: { id: "m1", checkCode: "stream-031", reason: "by design", mutedBy: "ops", unmutedBy: "" } }),
    ]);
    expect(groups.map((g) => g.severity)).toEqual(["critical", "info"]);
    expect(muted.map((m) => m.id)).toEqual(["m"]);
  });

  it("leaves resolved findings out", () => {
    const { groups } = groupCauses([f("r", "stream-007", streamEnt("a"), { state: "resolved" })]);
    expect(groups).toEqual([]);
  });
});

describe("parentKey", () => {
  it("is the stream for a consumer, the account for a stream, the system otherwise", () => {
    expect(parentKey(consumerEnt("order-jobs", "x"))).toBe("stream:APP/order-jobs");
    expect(parentKey(streamEnt("order-jobs"))).toBe("account:APP");
    expect(parentKey({ kind: "server", key: "nats-0", name: "nats-0", account: "", stream: "", consumer: "", server: "nats-0" })).toBe("system");
    expect(parentKey(undefined)).toBe("system");
  });
});

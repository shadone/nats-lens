import { describe, expect, it } from "vitest";
import type { Entity, Finding } from "../api/model";
import { acceptTarget, parentPhrase, relatedFindings, relatedQuery } from "./acceptRelated";
import { accountLabel } from "./view";

const consumer = (stream: string, name: string, account = "APP"): Entity => ({ kind: "consumer", key: `${account}/${stream}/${name}`, name, account, stream, consumer: name, server: "" });
const stream = (name: string, account = "APP"): Entity => ({ kind: "stream", key: `${account}/${name}`, name, account, stream: name, consumer: "", server: "" });

const finding = (e: Entity, over: Partial<Finding> = {}): Finding => ({
  id: `consumer-016:${e.key}`,
  checkCode: "consumer-016",
  checkName: "Consumer with one replica on a replicated stream",
  severity: "info",
  state: "firing",
  entity: e,
  detail: "",
  remediation: "",
  relatedConfig: [],
  relatedConfigSource: "",
  transitionsLastHour: 0,
  acceptable: true,
  ...over,
});

describe("relatedQuery", () => {
  it("narrows a consumer to its stream and a stream to its account", () => {
    expect(relatedQuery(consumer("order-jobs", "a"))?.entity).toMatchObject({ kind: "stream", key: "APP/order-jobs" });
    expect(relatedQuery(stream("orders"))).toEqual({ account: "APP" });
    expect(relatedQuery({ kind: "server", key: "nats-0", name: "nats-0", account: "", stream: "", consumer: "", server: "nats-0" })).toBeUndefined();
  });
});

describe("relatedFindings", () => {
  it("keeps the same check's firing, unmuted findings under the same parent, without the target", () => {
    const target = consumer("order-jobs", "a");
    const fs = [
      finding(target),
      finding(consumer("order-jobs", "b")),
      finding(consumer("order-jobs", "c"), { state: "resolved" }),
      finding(consumer("order-jobs", "d"), { mute: { id: "1", checkCode: "consumer-016", reason: "r", mutedBy: "op", accepted: true } as Finding["mute"] }),
      finding(consumer("returns", "e")),
      finding(consumer("order-jobs", "f"), { checkCode: "consumer-017" }),
      finding(consumer("order-jobs", "g"), { acceptable: false }),
    ];
    expect(relatedFindings(fs, "consumer-016", target, `consumer-016:${target.key}`).map((f) => f.entity!.consumer)).toEqual(["b"]);
  });

  it("keeps a stream's siblings in its own account only", () => {
    const target = stream("orders");
    const fs = [finding(stream("archive"), { checkCode: "stream-005" }), finding(stream("archive", "SUPPLIERS"), { checkCode: "stream-005" })];
    expect(relatedFindings(fs, "stream-005", target, "stream-005:APP/orders").map((f) => f.entity!.key)).toEqual(["APP/archive"]);
  });
});

describe("parentPhrase and acceptTarget", () => {
  it("names the parent", () => {
    expect(parentPhrase(consumer("order-jobs", "a"), accountLabel)).toBe("other consumers of stream order-jobs");
    expect(parentPhrase(stream("orders"), accountLabel)).toBe("other streams of account APP");
  });

  it("ticks the related entities only when asked", () => {
    const f = finding(consumer("order-jobs", "a"));
    expect(acceptTarget(f).selectAll).toBeUndefined();
    expect(acceptTarget(f, true).selectAll).toBe(true);
  });
});

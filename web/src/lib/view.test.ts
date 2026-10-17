import { describe, expect, it } from "vitest";
import type { Entity } from "../api/model";
import { create } from "@bufbuild/protobuf";
import { EntityType } from "../gen/natslens/v1/common_pb";
import { ConsumerSummarySchema } from "../gen/natslens/v1/consumers_pb";
import { StreamSummarySchema } from "../gen/natslens/v1/streams_pb";
import { consumer, stream } from "../api/adapt-entities";
import { accountLabel, accountLabeller, consumerCount, entityKeyLabel, isTrouble, withAccountName } from "./view";

describe("account names", () => {
  const id = "ACJQEXMPEXAMPLEAPPACCOUNTKEYQQQQQQQQQQQQQQQQQQQQQQQQQQQQ";
  it("shows the readable name in place of the account ID and keeps the ID", () => {
    const c = consumer(
      create(ConsumerSummarySchema, {
        entity: { type: EntityType.CONSUMER, key: `${id}/jobs/worker`, account: id, accountName: "APP", stream: "jobs", consumer: "worker" },
      }),
    );
    expect(c.entity.account).toBe(id);
    expect(entityKeyLabel(c.entity)).toBe("APP/jobs/worker");
    expect(accountLabel(c.entity.account, c.entity.accountName)).toBe("APP");
  });
  it("falls back to the ID without a name", () => {
    expect(accountLabel(id)).toBe(id);
    expect(entityKeyLabel({ kind: "stream", key: `${id}/jobs`, name: "jobs", account: id, stream: "jobs", consumer: "", server: "" })).toBe(`${id}/jobs`);
  });
});

describe("consumer counts", () => {
  it("shows an unknown count as a dash and is not short", () => {
    expect(consumerCount({ consumersExpected: 3 })).toEqual({ text: "–/3", short: false });
    expect(consumerCount({})).toEqual({ text: "–", short: false });
  });
  it("is short when fewer are present than expected", () => {
    expect(consumerCount({ consumers: 0, consumersExpected: 3 })).toEqual({ text: "0/3", short: true });
    expect(consumerCount({ consumers: 2 })).toEqual({ text: "2", short: false });
  });
  it("keeps an unset count unset in the adapter", () => {
    const s = stream(create(StreamSummarySchema, { entity: { type: EntityType.STREAM, key: "A/s", account: "A", stream: "s" }, consumersExpected: 3 }));
    expect(s.consumers).toBeUndefined();
    expect(stream(create(StreamSummarySchema, { consumers: 0 })).consumers).toBe(0);
  });
});

describe("account labels", () => {
  it("adds an ID prefix to a name two accounts share", () => {
    const label = accountLabeller([
      { account: "AAAA1111", accountName: "APP" },
      { account: "BBBB2222", accountName: "APP" },
      { account: "CCCC3333", accountName: "SYS" },
    ]);
    expect(label("AAAA1111", "APP")).toBe("APP (AAAA…)");
    expect(label("CCCC3333", "SYS")).toBe("SYS");
    expect(label("DDDD", undefined)).toBe("DDDD");
  });
  it("fills an entity's name from loaded data", () => {
    const e: Entity = { kind: "stream", key: "A1/s", name: "s", account: "A1", stream: "s", consumer: "", server: "" };
    expect(withAccountName(e, [undefined, { account: "A2", accountName: "X" }, { account: "A1", accountName: "APP" }]).accountName).toBe("APP");
    expect(withAccountName(e, []).accountName).toBeUndefined();
  });
});

describe("isTrouble", () => {
  it("is critical, warning, not collecting and summary unavailable; info is a notice", () => {
    expect(["critical", "warning", "notcollecting", "unavailable"].every((l) => isTrouble(l as never))).toBe(true);
    expect(["info", "ok", "muted", "nodata"].some((l) => isTrouble(l as never))).toBe(false);
  });
});

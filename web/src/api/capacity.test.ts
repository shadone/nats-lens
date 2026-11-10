import { create } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { CapacityStatus, CapacityStreamKind, GetCapacityResponseSchema, NextLimitKind, NextLimitState } from "../gen/natslens/v1/capacity_pb";
import { Severity } from "../gen/natslens/v1/common_pb";
import { capacity } from "./capacity";

const AT = Date.parse("2026-10-21T12:00:00Z");

describe("capacity", () => {
  it("reads memory stores, account limits and a stream's account limit", () => {
    const d = capacity(
      create(GetCapacityResponseSchema, {
        epoch: 7n,
        epochAt: timestampFromMs(AT),
        memoryStores: [{ serverName: "nats-0", cluster: "c", reported: true, used: 0n, perDay: 0 }],
        accountStores: [
          { account: "A", accountName: "APP", storage: "file", used: 400n, limit: 1000n, perDay: 100, daysToFull: 6 },
          { account: "A", storage: "memory", limit: 50n },
        ],
        streams: [
          { account: "A", stream: "open", accountLimit: 1000n, nextLimit: { limit: NextLimitKind.ACCOUNT, state: NextLimitState.REACHES, days: 6, discardNew: true } },
          { account: "A", stream: "plain" },
        ],
      }),
    );
    expect(d.memoryStores).toEqual([expect.objectContaining({ name: "nats-0", used: 0, limit: undefined, perDay: 0, findingId: "" })]);
    expect(d.accountStores).toEqual([
      { account: "A", accountName: "APP", storage: "file", used: 400, limit: 1000, perDay: 100, daysToFull: 6 },
      { account: "A", accountName: "", storage: "memory", used: undefined, limit: 50, perDay: undefined, daysToFull: undefined },
    ]);
    expect(d.streams[0].accountLimit).toBe(1000);
    expect(d.streams[0].next).toEqual({ limit: "account", state: "reaches", days: 6, discardNew: true });
    expect(d.streams[1].accountLimit).toBeUndefined();
  });


  it("reads an empty response as no collection", () => {
    const d = capacity(create(GetCapacityResponseSchema, {}));
    expect(d.collected).toBe(false);
    expect(d.epochAt).toBeUndefined();
    expect(d.servers).toEqual([]);
    expect(d.streams).toEqual([]);
  });

  it("keeps unset values undefined and set ones, 0 among them, as numbers", () => {
    const d = capacity(
      create(GetCapacityResponseSchema, {
        epoch: 7n,
        epochAt: timestampFromMs(AT),
        growthFrom: timestampFromMs(AT - 86_400_000),
        servers: [
          { serverName: "nats-0", cluster: "c", reported: true, used: 0n, limit: 10n, perDay: 0, findingId: "server-014:nats-0", findingCode: "server-014", findingSeverity: Severity.WARNING, findingDays: 9 },
          { serverName: "nats-1", cluster: "c" },
        ],
        serversWithoutJetstream: 2,
        streams: [
          {
            account: "APP",
            stream: "KV_a",
            kind: CapacityStreamKind.KEY_VALUE,
            replicas: 3,
            messages: 0n,
            bytes: 0n,
            keys: 0n,
            maxAge: durationFromMs(172_800_000),
            maxMsgsPerSubject: 1n,
            bytesPerDay: 0,
            nextLimit: { limit: NextLimitKind.AGE, state: NextLimitState.REACHES, days: 2, discardNew: true },
            status: CapacityStatus.ACCEPTED,
            findingId: "stream-005:APP/KV_a",
            mute: { id: "1", checkCode: "stream-005", accepted: true, growthPerDay: 0, endReason: "" },
            declaredReason: "",
          },
          { account: "APP", stream: "b" },
        ],
        streamsTotal: 2,
      }),
    );
    expect(d.collected).toBe(true);
    expect(d.growthFrom).toBe(AT - 86_400_000);
    expect(d.servers[0]).toMatchObject({ used: 0, limit: 10, perDay: 0, findingSeverity: "warning", findingDays: 9 });
    expect(d.servers[1]).toMatchObject({ reported: false, used: undefined, limit: undefined, perDay: undefined, daysToFull: undefined, findingSeverity: undefined });
    expect(d.serversWithoutJetstream).toBe(2);
    expect(d.streams[0]).toMatchObject({ kind: "kv", messages: 0, bytes: 0, keys: 0, maxAge: 172_800_000, maxMsgsPerSubject: 1, bytesPerDay: 0, status: "accepted" });
    expect(d.streams[0].next).toEqual({ limit: "age", state: "reaches", days: 2, discardNew: true });
    expect(d.streams[0].mute).toMatchObject({ accepted: true, growthPerDay: 0 });
    expect(d.streams[1]).toMatchObject({ kind: "stream", messages: undefined, bytes: undefined, firstTs: undefined, maxAge: undefined, maxMsgs: undefined, bytesPerDay: undefined, mute: undefined, status: "unknown" });
    expect(d.streams[1].next).toEqual({ limit: "unknown", state: undefined, days: undefined, discardNew: false });
  });
});

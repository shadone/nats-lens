import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { GetSubjectsResponseSchema, MatchSubjectResponseSchema, SubjectFilter, SubjectRowKind } from "../gen/natslens/v1/subjects_pb";
import { FILTER_ENUM, subjectMatches, subjects } from "./subjects";

const AT = Date.parse("2026-10-21T12:00:00Z");

describe("subjects", () => {
  it("reads an empty response as no collection", () => {
    const d = subjects(create(GetSubjectsResponseSchema, {}));
    expect(d.collected).toBe(false);
    expect(d.epochAt).toBeUndefined();
    expect(d.subscriptionsAt).toBeUndefined();
    expect(d.rows).toEqual([]);
    expect(d.counts).toEqual({ all: 0, stored: 0, noConsumer: 0, overlapping: 0, wide: 0 });
  });

  it("keeps unset values undefined and set ones, 0 among them, as numbers", () => {
    const d = subjects(
      create(GetSubjectsResponseSchema, {
        epoch: 7n,
        epochAt: timestampFromMs(AT),
        subscriptionsAt: timestampFromMs(AT - 90_000),
        rows: [
          { account: "APP", kind: SubjectRowKind.STREAM, patterns: ["a.>"], stream: "a", retention: "workqueue", messages: 0n, durableConsumers: 1, storedPerSecond: 0 },
          { account: "APP", kind: SubjectRowKind.KEY_VALUE, patterns: ["$KV.b.>"], stream: "KV_b", ephemeralConsumers: 2, clientNames: ["x"], moreNames: 1, mirroredBy: ["m"] },
          { account: "SYS", kind: SubjectRowKind.SUBSCRIPTION, patterns: [">"], connections: 1, wide: true, systemAccount: true },
          { account: "APP", kind: SubjectRowKind.INBOX, patterns: ["_INBOX.>"], connections: 3 },
          { account: "APP", kind: SubjectRowKind.JETSTREAM_API, patterns: ["$JS.API.>"], apiPerSecond: 1.5, apiErrorShare: 0 },
          { account: "APP", kind: SubjectRowKind.OBJECT_STORE, overlaps: ["c"], noConsumer: true },
        ],
        rowsTotal: 1204,
        allCount: 1204,
        storedCount: 3,
        noConsumerCount: 1,
        overlappingCount: 1,
        wideCount: 1,
        subszServers: [{ serverName: "nats-0", answered: true, total: 14212n, listed: 10000n }, { serverName: "nats-1" }],
        serverSubscriptions: 1006n,
        unlistedConnectionSubscriptions: 4n,
      }),
    );
    expect(d.collected).toBe(true);
    expect(d.epochAt).toBe(AT);
    expect(d.subscriptionsAt).toBe(AT - 90_000);
    expect(d.rows.map((r) => r.kind)).toEqual(["stream", "kv", "subscription", "inbox", "jsApi", "object"]);
    expect(d.rows[0]).toMatchObject({ messages: 0, storedPerSecond: 0, durable: 1, retention: "workqueue" });
    expect(d.rows[1]).toMatchObject({ messages: undefined, storedPerSecond: undefined, ephemeral: 2, names: ["x"], moreNames: 1, mirroredBy: ["m"] });
    expect(d.rows[2]).toMatchObject({ connections: 1, wide: true, systemAccount: true });
    expect(d.rows[4]).toMatchObject({ apiPerSecond: 1.5, apiErrorShare: 0 });
    expect(d.rows[3].apiPerSecond).toBeUndefined();
    expect(d.rows[5]).toMatchObject({ overlaps: ["c"], noConsumer: true });
    expect(d.rowsTotal).toBe(1204);
    expect(d.counts).toEqual({ all: 1204, stored: 3, noConsumer: 1, overlapping: 1, wide: 1 });
    expect(d.servers).toEqual([
      { name: "nats-0", answered: true, total: 14212, listed: 10000 },
      { name: "nats-1", answered: false, total: undefined, listed: undefined },
    ]);
    expect(d.serverSubscriptions).toBe(1006);
    expect(d.unlistedSubscriptions).toBe(4);
  });

  it("maps every filter to the request's enum", () => {
    expect(FILTER_ENUM).toEqual({
      all: SubjectFilter.UNSPECIFIED,
      stored: SubjectFilter.STORED,
      noConsumer: SubjectFilter.NO_CONSUMER,
      overlapping: SubjectFilter.OVERLAPPING,
      wide: SubjectFilter.WIDE,
    });
  });
});

describe("subjectMatches", () => {
  it("reads each account's streams, subscribers and JetStream API", () => {
    const m = subjectMatches(
      create(MatchSubjectResponseSchema, {
        epoch: 7n,
        accounts: [
          {
            account: "APP",
            matches: [{ account: "APP", stream: "order-events", pattern: "order.events.>", consumers: ["a"], ephemeralConsumers: 2, mirrors: ["order-archive"], sourcedBy: ["s"] }],
            subscribers: [{ account: "APP", subject: "orders.place", queue: "order-service", connections: 3, clientNames: ["order-service"], moreNames: 0 }],
          },
          { account: "SYS", accountName: "system", systemAccount: true, jetstreamApi: true },
        ],
        subscriptionsAt: timestampFromMs(AT),
      }),
    );
    expect(m).toEqual({
      collected: true,
      accounts: [
        {
          account: "APP",
          accountName: "",
          systemAccount: false,
          streams: [{ account: "APP", accountName: "", stream: "order-events", pattern: "order.events.>", consumers: ["a"], ephemeral: 2, mirrors: ["order-archive"], sourcedBy: ["s"] }],
          core: [{ account: "APP", accountName: "", subject: "orders.place", queue: "order-service", connections: 3, names: ["order-service"], moreNames: 0 }],
          jetstreamApi: false,
        },
        { account: "SYS", accountName: "system", systemAccount: true, streams: [], core: [], jetstreamApi: true },
      ],
      subscriptionsAt: AT,
    });
  });

  it("reads no epoch as no collection and no subscription collection as unknown subscribers", () => {
    const m = subjectMatches(create(MatchSubjectResponseSchema, {}));
    expect(m.collected).toBe(false);
    expect(m.accounts).toEqual([]);
    expect(m.subscriptionsAt).toBeUndefined();
  });
});

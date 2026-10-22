import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { GetMicroServicesResponseSchema, MicroInstanceSource, MicroNote, MicroRequestState } from "../gen/natslens/v1/micro_pb";
import { microServices } from "./micro";

const AT = Date.parse("2026-10-17T14:30:00Z");

describe("microServices", () => {
  it("reads an empty response as no collection", () => {
    const d = microServices(create(GetMicroServicesResponseSchema, {}));
    expect(d).toEqual({ collected: false, epochAt: undefined, subscriptionsAt: undefined, services: [], servicesTotal: 0, accounts: [] });
  });

  it("maps each instance source", () => {
    const d = microServices(
      create(GetMicroServicesResponseSchema, {
        epoch: 7n,
        epochAt: timestampFromMs(AT),
        services: [
          { name: "a", instanceSource: MicroInstanceSource.ANSWERED },
          { name: "b", instanceSource: MicroInstanceSource.SUBSCRIPTIONS },
          { name: "c", instanceSource: MicroInstanceSource.LAST_ANSWERED },
          { name: "d", instanceSource: MicroInstanceSource.UNSPECIFIED },
        ],
      }),
    );
    expect(d.services.map((s) => [s.name, s.instanceSource])).toEqual([
      ["a", "answered"],
      ["b", "subscriptions"],
      ["c", "lastAnswered"],
      ["d", "none"],
    ]);
  });

  it("keeps an endpoint's subjects, and its one subject when the list is empty", () => {
    const d = microServices(
      create(GetMicroServicesResponseSchema, {
        epoch: 7n,
        services: [
          {
            name: "orders",
            note: MicroNote.SLOW_ENDPOINT,
            endpoints: [
              { name: "create", subject: "orders.create", subjects: ["orders.create", "orders.create.v2"] },
              { name: "get", subject: "orders.get.*" },
              { name: "none" },
            ],
          },
        ],
        accounts: [{ account: "APP", asked: true, stats: { state: MicroRequestState.TIMEOUT, at: timestampFromMs(AT) } }],
      }),
    );
    expect(d.services[0].endpoints.map((e) => e.subjects)).toEqual([["orders.create", "orders.create.v2"], ["orders.get.*"], []]);
    expect(d.services[0].note).toBe("slow");
    expect(d.accounts[0].stats).toEqual({ state: "timeout", at: AT, replies: 0, error: "" });
    expect(d.accounts[0].info).toEqual({ state: "notAsked", replies: 0, error: "" });
  });
  it("reads a held request with when it was refused and when it is asked again, apart from an error and a timeout", () => {
    const refusal = 'nats: Permissions Violation for Publish to "$SRV.STATS"';
    const d = microServices(
      create(GetMicroServicesResponseSchema, {
        epoch: 7n,
        accounts: [
          {
            account: "A",
            asked: true,
            stats: { state: MicroRequestState.HELD, at: timestampFromMs(AT), error: refusal, refusedAt: timestampFromMs(AT - 360_000), retryAfter: timestampFromMs(AT + 240_000) },
          },
          { account: "B", asked: true, stats: { state: MicroRequestState.ERROR, at: timestampFromMs(AT), error: refusal } },
          { account: "C", asked: true, stats: { state: MicroRequestState.TIMEOUT, at: timestampFromMs(AT) } },
        ],
        services: [{ name: "inventory", missing: [{ id: "I1", server: "nats-eu-5", stillSubscribed: true }, { id: "I2" }] }],
      }),
    );
    expect(d.accounts.map((a) => a.stats)).toEqual([
      { state: "held", at: AT, replies: 0, error: refusal, refusedAt: AT - 360_000, againAfter: AT + 240_000 },
      { state: "error", at: AT, replies: 0, error: refusal, refusedAt: undefined, againAfter: undefined },
      { state: "timeout", at: AT, replies: 0, error: "", refusedAt: undefined, againAfter: undefined },
    ]);
    expect(d.services[0].missing.map((m) => m.server)).toEqual(["nats-eu-5", ""]);
  });
});

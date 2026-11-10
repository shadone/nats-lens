import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { GetApplicationsResponseSchema } from "../gen/natslens/v1/applications_pb";
import { Severity } from "../gen/natslens/v1/common_pb";
import { applications } from "./applications";

const AT = Date.parse("2026-10-17T14:29:56Z");
const HOUR = Date.parse("2026-10-17T13:00:00Z");

describe("applications", () => {
  it("reads an empty response as no collection", () => {
    const d = applications(create(GetApplicationsResponseSchema, {}));
    expect(d.collected).toBe(false);
    expect([d.partial, d.missing, d.captureGaps]).toEqual([false, [], false]);
    expect(d.applications).toEqual([]);
    expect(d.spread).toEqual({ server: "", connections: 0, total: 0, servers: 0, concentrated: false });
    expect(d.lastDay).toEqual({ slowConsumers: undefined, stale: undefined, reconnecting: undefined });
  });

  it("keeps a value the server left unset undefined, never 0", () => {
    const d = applications(
      create(GetApplicationsResponseSchema, {
        epoch: 7n,
        epochAt: timestampFromMs(AT),
        applications: [{ name: "order-service", connections: 2, traffic: [{ hour: timestampFromMs(HOUR) }] }],
        lastDay: {},
      }),
    );
    const a = d.applications[0];
    expect([a.rttMs, a.subscriptions, a.pendingBytes, a.pendingBytesMax, a.reconnects, a.slowConsumers, a.stale, a.shortLived, a.inMsgs, a.outMsgs]).toEqual(Array(10).fill(undefined));
    expect(a.traffic).toEqual([{ hour: HOUR, inMsgs: undefined, outMsgs: undefined, inBytes: undefined, outBytes: undefined }]);
    expect(d.lastDay).toEqual({ slowConsumers: undefined, stale: undefined, reconnecting: undefined });
  });

  it("reads a set zero as zero, a round trip below a millisecond with its fraction, and findings with their severity", () => {
    const d = applications(
      create(GetApplicationsResponseSchema, {
        epoch: 7n,
        eventsCaptured: true,
        connzCut: true,
        toolingHidden: 2,
        applications: [
          {
            name: "callback-dispatcher",
            rttP50: { seconds: 0n, nanos: 690_000 },
            subscriptions: 0n,
            pendingBytes: 0n,
            reconnects24h: 6,
            stale24h: 0,
            inMsgs24h: 3_558_520n,
            findings: [{ id: "client-004:callback-dispatcher", checkCode: "client-004", severity: Severity.WARNING, muted: true, entityKey: "callback-dispatcher" }],
            traffic: [{ hour: timestampFromMs(HOUR), inMsgs: 0n, outMsgs: 12n }],
            connectionNames: ["callback-dispatcher"],
            moreNames: 2,
          },
        ],
        spread: { server: "nats-0", connections: 100, total: 187, servers: 3, concentrated: true },
        libraries: [{ lang: "nats.js", version: "3.0.2", applications: 6 }],
        lastDay: { slowConsumers: 0, stale: 6, reconnecting: 1 },
        partial: true,
        missing: ["nats-1"],
        captureGaps: true,
      }),
    );
    const a = d.applications[0];
    expect([d.partial, d.missing, d.captureGaps]).toEqual([true, ["nats-1"], true]);
    expect(a.rttMs).toBeCloseTo(0.69, 6);
    expect([a.connectionNames, a.moreNames]).toEqual([["callback-dispatcher"], 2]);
    expect([a.subscriptions, a.pendingBytes, a.reconnects, a.stale, a.inMsgs]).toEqual([0, 0, 6, 0, 3_558_520]);
    expect(a.findings).toEqual([{ id: "client-004:callback-dispatcher", checkCode: "client-004", checkName: "", severity: "warning", muted: true, entityKey: "callback-dispatcher" }]);
    expect(a.traffic[0]).toEqual({ hour: HOUR, inMsgs: 0, outMsgs: 12, inBytes: undefined, outBytes: undefined });
    expect([d.connzCut, d.eventsCaptured, d.toolingHidden]).toEqual([true, true, 2]);
    expect(d.spread.concentrated).toBe(true);
    expect(d.lastDay).toEqual({ slowConsumers: 0, stale: 6, reconnecting: 1 });
  });
});

describe("applications Runs on", () => {
  it("reads Runs on and the failing watches, and leaves Runs on undefined while client pods are not watched", () => {
    const d = applications(
      create(GetApplicationsResponseSchema, {
        epoch: 7n,
        epochAt: timestampFromMs(AT),
        clientPodsWatched: true,
        clientPodsUnwatched: [{ source: "client pods in shop", since: timestampFromMs(AT - 60_000), error: "Kubernetes API returned HTTP 403" }],
        applications: [
          { name: "cart", connections: 3, runsOn: { matched: 2, ambiguous: 1, workloads: [{ namespace: "shop", kind: "Deployment", name: "cart", pods: 2, connections: 2 }], workloadsTotal: 1, nodes: ["n1"], nodesTotal: 1, pods: 2 } },
          { name: "ledger", connections: 1 },
        ],
      }),
    );
    expect(d.clientPodsWatched).toBe(true);
    expect(d.clientPodsCapped).toBe(false);
    expect(d.clientPodsUnwatched).toEqual([{ source: "client pods in shop", since: AT - 60_000, error: "Kubernetes API returned HTTP 403" }]);
    expect(d.applications[0].runsOn).toEqual({
      matched: 2,
      ambiguous: 1,
      workloads: [{ namespace: "shop", kind: "Deployment", name: "cart", pods: 2, connections: 2 }],
      workloadsTotal: 1,
      nodes: ["n1"],
      nodesTotal: 1,
      pods: 2,
    });
    expect(d.applications[1].runsOn).toBeUndefined();
    expect(applications(create(GetApplicationsResponseSchema, {})).clientPodsWatched).toBe(false);
  });
});

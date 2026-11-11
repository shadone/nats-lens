import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { EntityType, Status } from "../gen/natslens/v1/common_pb";
import { ConsumerSummarySchema } from "../gen/natslens/v1/consumers_pb";
import { EventSchema, EventSource } from "../gen/natslens/v1/events_pb";
import { GetStatusTimelineResponseSchema } from "../gen/natslens/v1/timeline_pb";
import { consumer } from "./adapt-entities";
import { event, timeline } from "./adapt-findings";

describe("adapters", () => {
  it("leaves the backlog of a missing consumer unset instead of 0", () => {
    const c = consumer(
      create(ConsumerSummarySchema, {
        entity: { type: EntityType.CONSUMER, key: "A/s/c", account: "A", stream: "s", consumer: "c" },
        present: false,
        status: Status.CRITICAL,
      }),
    );
    expect(c.numPending).toBeUndefined();
    expect(c.status).toBe("critical");
    expect(c.entity.kind).toBe("consumer");
  });

  it("keeps the backlog of a present consumer, and leaves an unobserved one unset", () => {
    const c = consumer(create(ConsumerSummarySchema, { present: true, numPending: 18420n, numAckPending: 0n }));
    expect(c.numPending).toBe(18420);
    expect(c.numWaiting).toBeUndefined();
    expect(c.numAckPending).toBe(0);
  });

  it("prefers the keys the API gives", () => {
    const tl = timeline(create(GetStatusTimelineResponseSchema, { rows: [{ checkCode: "c-1", key: "c-1:A/s" }], nats: [{ id: "e9", eventId: "9" }] }));
    expect(tl.rows[0].key).toBe("c-1:A/s");
    expect(tl.nats[0]).toMatchObject({ id: "e9", eventId: "9" });
  });

  it("turns event IDs into strings and sources into names", () => {
    const e = event(create(EventSchema, { id: "207", source: EventSource.KUBERNETES, time: timestampFromMs(1000) }));
    expect(e.id).toBe("207");
    expect(e.source).toBe("kubernetes");
    expect(e.time).toBe(1000);
    expect(e.severity).toBeUndefined();
  });

  it("maps timeline cells and gives rows and lane events keys", () => {
    const tl = timeline(
      create(GetStatusTimelineResponseSchema, {
        bucketCount: 2,
        rows: [{ checkCode: "server-002", cells: [Status.OK, Status.NO_DATA], now: Status.UNSPECIFIED }],
        kubernetes: [{ id: "k1", time: timestampFromMs(5000), kind: "k8s.pod.killing", label: "pod nats-0 Killing" }],
      }),
    );
    expect(tl.rows[0].cells).toEqual(["ok", "nodata"]);
    expect(tl.rows[0].now).toBe("nodata");
    expect(tl.rows[0].key).toBe("server-002");
    expect(tl.kubernetes[0].id).toBe("k1");
  });
});

describe("numbers a server did not report", () => {
  it("stay unset for a server, a stream and a connection", async () => {
    const { ServerSummarySchema, ConnectionSchema } = await import("../gen/natslens/v1/servers_pb");
    const { StreamSummarySchema } = await import("../gen/natslens/v1/streams_pb");
    const { server, stream, connection } = await import("./adapt-entities");
    const s = server(create(ServerSummarySchema, { name: "nats-2", present: false }));
    expect([s.connections, s.jsStorageBytes, s.memBytes, s.cpuPercent]).toEqual([undefined, undefined, undefined, undefined]);
    const st = stream(create(StreamSummarySchema, {}));
    expect([st.replicas, st.messages, st.bytes, st.bytesPerDay]).toEqual([undefined, undefined, undefined, undefined]);
    const c = connection(create(ConnectionSchema, { server: "nats-0", cid: 4n }));
    expect([c.pendingBytes, c.inMsgs, c.stalls, c.port]).toEqual([undefined, undefined, undefined, undefined]);
  });
});

describe("finding", () => {
  it("carries the API's severity reason, and none when it is empty", async () => {
    const { FindingSchema } = await import("../gen/natslens/v1/findings_pb");
    const { finding } = await import("./adapt-findings");
    const reason = "Every stream-001 finding is critical; its severity does not change.";
    expect(finding(create(FindingSchema, { checkCode: "stream-001", severityReason: reason })).severityReason).toBe(reason);
    expect("severityReason" in finding(create(FindingSchema, { checkCode: "stream-001" }))).toBe(false);
  });
});

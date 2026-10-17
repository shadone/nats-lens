import { create } from "@bufbuild/protobuf";
import { durationFromMs, timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { Status } from "../gen/natslens/v1/common_pb";
import { CollectionState, SystemSummarySchema } from "../gen/natslens/v1/systems_pb";
import { systemSummary } from "./adapt-systems";

describe("systemSummary", () => {
  it("reads the collection state, its error, the counts and the strip", () => {
    const s = systemSummary(
      create(SystemSummarySchema, {
        system: { id: "example-staging", name: "", environment: "staging" },
        collection: CollectionState.CONNECTION_CLOSED,
        collectionError: "nats: authorization violation",
        collectionErrorSince: timestampFromMs(1_000_000),
        status: Status.INFO,
        servers: 3,
        serversAnswering: 2,
        connections: 16n,
        strip: [Status.OK, Status.NO_DATA],
        stripStart: timestampFromMs(5_000),
        stripBucket: durationFromMs(1_800_000),
      }),
    );
    expect(s).toMatchObject({
      id: "example-staging",
      name: "example-staging",
      environment: "staging",
      collection: "connection closed",
      collectionError: "nats: authorization violation",
      collectionErrorSince: 1_000_000,
      status: "info",
      servers: 3,
      serversAnswering: 2,
      connections: 16,
      strip: ["ok", "nodata"],
      stripStart: 5_000,
      stripBucketMs: 1_800_000,
      error: "",
    });
  });

  it("leaves counts that were not observed unset, never 0", () => {
    const s = systemSummary(create(SystemSummarySchema, { system: { id: "a" }, error: "deadline exceeded" }));
    expect(s.servers).toBeUndefined();
    expect(s.clusters).toBeUndefined();
    expect(s.streams).toBeUndefined();
    expect(s.connections).toBeUndefined();
    expect(s.error).toBe("deadline exceeded");
  });
});

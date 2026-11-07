import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { EntityType, PlacementState } from "../gen/natslens/v1/common_pb";
import { GetPlacementResponseSchema, PlacementFilter } from "../gen/natslens/v1/placement_pb";
import { ServerRole } from "../gen/natslens/v1/servers_pb";
import { FILTER_ENUM, matrix, matrixFilter } from "./placementMatrix";

describe("matrix", () => {
  it("adapts servers, assets, cells and counts", () => {
    const m = matrix(
      create(GetPlacementResponseSchema, {
        servers: [
          { name: "nats-0", streamLeaders: 4, consumerLeaders: 2, memBytes: 1024n, role: ServerRole.META_LEADER, present: true },
          { name: "nats-1", role: ServerRole.FOLLOWER, present: false },
        ],
        streams: [
          {
            entity: { type: EntityType.STREAM, key: "APP/orders", account: "APP", stream: "orders", name: "orders" },
            replicas: 3,
            leader: "nats-0",
            unplaced: 1,
            messages: 7378n,
            matches: true,
            cells: [{ server: "nats-0", state: PlacementState.LEADER, lastSeq: 7378n, peers: ["nats-1"], created: timestampFromMs(1000) }],
            consumers: [
              {
                entity: { type: EntityType.CONSUMER, key: "APP/orders/ship", account: "APP", stream: "orders", consumer: "ship", name: "ship" },
                durable: true,
                cells: [{ server: "nats-1", state: PlacementState.IN_SYNC, deliveredStreamSeq: 9n, ackFloorStreamSeq: 8n, lag: 0n, differs: ["created"] }],
              },
            ],
          },
        ],
        counts: { all: 2, serversDisagree: 1, replicaBehind: 0, missing: 1, singleReplica: 0 },
        epochAt: timestampFromMs(5000),
      }),
    );
    expect(m.servers).toEqual([
      { name: "nats-0", leads: 6, memBytes: 1024, uptimeMs: undefined, metaLeader: true, present: true },
      { name: "nats-1", leads: undefined, memBytes: undefined, uptimeMs: undefined, metaLeader: false, present: false },
    ]);
    const s = m.streams[0];
    expect(s.entity.kind).toBe("stream");
    expect([s.replicas, s.unplaced, s.messages, s.matches]).toEqual([3, 1, 7378, true]);
    expect(s.cells[0]).toMatchObject({ server: "nats-0", state: "leader", lastSeq: 7378, created: 1000, peers: ["nats-1"] });
    expect(s.consumers[0].cells[0]).toMatchObject({ state: "in_sync", delivered: 9, ackFloor: 8, lag: 0, differs: ["created"] });
    expect(m.counts).toEqual({ all: 2, disagree: 1, behind: 0, missing: 1, single: 0 });
    expect(m.epochAt).toBe(5000);
  });

  it("names each filter in the URL and the API", () => {
    expect(matrixFilter("disagree")).toBe("disagree");
    expect(matrixFilter("nonsense")).toBe("all");
    expect(matrixFilter(null)).toBe("all");
    expect(FILTER_ENUM.single).toBe(PlacementFilter.SINGLE_REPLICA);
  });
});

it("keeps an observed zero span distinct from absent history", () => {
  const m = matrix(create(GetPlacementResponseSchema, { epochAt: timestampFromMs(0), observation: { asset: "APP/orders", server: "nats-1", asOf: timestampFromMs(0), since: timestampFromMs(0), samples: 1, state: PlacementState.BEHIND, boundary: "evidence_unknown" } }));
  expect(m.observation).toMatchObject({ since: 0, asOf: 0, samples: 1, state: "behind" });
  expect(matrix(create(GetPlacementResponseSchema)).observation).toBeUndefined();
});

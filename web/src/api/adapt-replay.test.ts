import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { GetReplayResponseSchema, ReplayUnobservedReason } from "../gen/natslens/v1/replay_pb";
import { replay } from "./adapt-replay";

describe("replay adapter", () => {
  it("carries why each unset count is unset, with the field named as the view model names it", () => {
    const r = create(GetReplayResponseSchema, {
      unobserved: [
        { count: "leaders_moved", reason: ReplayUnobservedReason.COLLECTOR_GAP, from: timestampFromMs(1000), to: timestampFromMs(2000) },
        { count: "nodes_drained", reason: ReplayUnobservedReason.KUBERNETES_NOT_WATCHED },
        { count: "findings_raised" },
      ],
    });
    expect(replay(r, { system: "", a: 0, b: 1 }).unobserved).toEqual([
      { count: "leadersMoved", reason: "collector_gap", from: 1000, to: 2000 },
      { count: "nodesDrained", reason: "kubernetes_not_watched", from: undefined, to: undefined },
      { count: "findingsRaised", reason: undefined, from: undefined, to: undefined },
    ]);
  });
});

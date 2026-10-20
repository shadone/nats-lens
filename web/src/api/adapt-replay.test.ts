import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { GetReplayResponseSchema, ReplayEvidenceKind, ReplayUnobservedReason } from "../gen/natslens/v1/replay_pb";
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

  it("carries the server an election elected and the step's leaders taken", () => {
    const r = create(GetReplayResponseSchema, {
      steps: [
        {
          id: "restart:1",
          evidence: [
            { kind: ReplayEvidenceKind.EVENT, id: "1", label: "order-jobs -> nats-0", elected: "nats-0" },
            { kind: ReplayEvidenceKind.EVENT, id: "2", label: "pod nats-1 Killing" },
          ],
          leadersTaken: [{ leader: "nats-0", count: 22 }],
        },
      ],
    });
    const s = replay(r, { system: "", a: 0, b: 1 }).steps[0];
    expect(s.evidence.map((e) => e.elected)).toEqual(["nats-0", undefined]);
    expect(s.leadersTaken).toEqual([{ leader: "nats-0", count: 22 }]);
  });
});

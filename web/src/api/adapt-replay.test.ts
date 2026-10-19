import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { GetReplayResponseSchema, ReplayGapKind, ReplayStepKind, ReplayUnobservedReason } from "../gen/natslens/v1/replay_pb";
import { replay } from "./adapt-replay";

describe("replay adapter", () => {
  it("carries a gap step's kind, and leaves an unspecified one unset", () => {
    const r = create(GetReplayResponseSchema, {
      steps: [
        { id: "gap:1", kind: ReplayStepKind.GAP, title: "No collections from 09:20 to 09:22", gapKind: ReplayGapKind.COLLECTIONS },
        { id: "gap:2", kind: ReplayStepKind.GAP, title: "nats-lens was not running", gapKind: ReplayGapKind.COLLECTOR },
        { id: "gap:3", kind: ReplayStepKind.GAP, title: "Kubernetes was not watched", gapKind: ReplayGapKind.KUBERNETES },
        { id: "gap:4", kind: ReplayStepKind.GAP, title: "No collections since 09:30" },
      ],
    });
    expect(replay(r, { system: "", a: 0, b: 1 }).steps.map((s) => s.gapKind)).toEqual(["collections", "collector", "kubernetes", undefined]);
  });

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

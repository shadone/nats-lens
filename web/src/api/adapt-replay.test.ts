import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import { GetReplayResponseSchema, ReplayGapKind, ReplayStepKind } from "../gen/natslens/v1/replay_pb";
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
});

// Replica placement: proto to view model.
import { PlacementState, type ReplicaPlacement } from "../gen/natslens/v1/common_pb";

export type PlacementStateName = "leader" | "in_sync" | "behind" | "disagrees" | "missing" | "offline" | "unknown";

/** One replica of a stream: the server that holds it and its state. */
export interface Placement {
  server: string;
  state: PlacementStateName;
}

const STATE: Record<PlacementState, PlacementStateName> = {
  [PlacementState.UNSPECIFIED]: "unknown",
  [PlacementState.LEADER]: "leader",
  [PlacementState.IN_SYNC]: "in_sync",
  [PlacementState.BEHIND]: "behind",
  [PlacementState.DISAGREES]: "disagrees",
  [PlacementState.MISSING]: "missing",
  [PlacementState.OFFLINE]: "offline",
};

export function placement(ps: ReplicaPlacement[]): Placement[] {
  return ps.map((p) => ({ server: p.server, state: STATE[p.state] ?? "unknown" }));
}

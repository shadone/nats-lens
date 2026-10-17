// Replica placement: one square per replica, leader first, and a legend
// that draws the same square for each state it names.
import type { Placement, PlacementStateName } from "../api/adapt-placement";

export const PLACEMENT_LABEL: Record<PlacementStateName, string> = {
  leader: "leader",
  in_sync: "in sync",
  behind: "behind",
  disagrees: "disagrees",
  missing: "missing",
  offline: "offline",
  unknown: "state unknown",
};

const ORDER: PlacementStateName[] = ["leader", "in_sync", "behind", "disagrees", "offline", "missing", "unknown"];

/** The mark of one state, drawn in a 12 by 12 box. */
function Mark({ state }: { state: PlacementStateName }) {
  switch (state) {
    case "leader":
      return <rect x="0" y="0" width="12" height="12" rx="3" className="pl-leader" />;
    case "in_sync":
      return <rect x="0" y="0" width="12" height="12" rx="3" className="pl-sync" />;
    case "behind":
      // Half full: the replica has part of the leader's data.
      return (
        <>
          <rect x=".75" y=".75" width="10.5" height="10.5" rx="2.5" className="pl-behind-o" />
          <path d="M.75 6h10.5v2.75a2.5 2.5 0 0 1-2.5 2.5h-5.5a2.5 2.5 0 0 1-2.5-2.5Z" className="pl-behind" />
        </>
      );
    case "disagrees":
      return (
        <>
          <rect x="0" y="0" width="12" height="12" rx="3" className="pl-disagree" />
          <path d="M3.5 4.5h5M3.5 7.5h5" className="pl-disagree-m" />
        </>
      );
    case "offline":
      return (
        <>
          <rect x="0" y="0" width="12" height="12" rx="3" className="pl-offline" />
          <path d="M3.8 3.8l4.4 4.4M8.2 3.8l-4.4 4.4" className="pl-offline-m" />
        </>
      );
    case "missing":
      return <rect x=".75" y=".75" width="10.5" height="10.5" rx="2.5" className="pl-missing" />;
    default:
      return <rect x=".75" y=".75" width="10.5" height="10.5" rx="2.5" className="pl-unknown" />;
  }
}

export function PlacementMark({ state }: { state: PlacementStateName }) {
  return (
    <svg className="pl-sq" viewBox="0 0 12 12" aria-hidden="true">
      <Mark state={state} />
    </svg>
  );
}

/** "nats-0 leader, nats-1 in sync" */
export function placementText(ps: Placement[]): string {
  return ps.map((p) => `${p.server} ${PLACEMENT_LABEL[p.state]}`).join(", ");
}

export function PlacementSquares({ placement }: { placement: Placement[] }) {
  if (placement.length === 0) return null;
  return (
    <span className="pl" role="img" aria-label={placementText(placement)} title={placementText(placement)}>
      {placement.map((p, i) => (
        <PlacementMark key={`${p.server}-${i}`} state={p.state} />
      ))}
    </span>
  );
}

/** A legend of the states that occur, each drawn with its own mark. */
export function PlacementLegend({ states }: { states: Iterable<PlacementStateName> }) {
  const seen = new Set(states);
  const shown = ORDER.filter((s) => seen.has(s));
  if (shown.length === 0) return null;
  return (
    <ul className="pl-legend" aria-label="Placement legend">
      {shown.map((s) => (
        <li key={s}>
          <PlacementMark state={s} />
          {PLACEMENT_LABEL[s]}
        </li>
      ))}
      <li className="pl-note">one square per replica, leader first</li>
    </ul>
  );
}

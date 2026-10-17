// Fleet: a card per system, worst first, each opening that system's
// Overview. It reads every system, so it has no ?system= of its own.
import type { Level } from "../api/model";
import { useSystemSummaries } from "../api/systems";
import { Empty, QueryView } from "../components/common";
import { FleetCard, StripLegend } from "../components/FleetCard";
import { worstFirst } from "../lib/fleet";
import { dayMonHm } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useNow, useTimeRange } from "../lib/useTimeRange";

export function Fleet() {
  usePageTitle("Fleet");
  const { spec } = useTimeRange();
  const live = spec.kind === "live";
  const now = useNow(5_000, live);
  const q = useSystemSummaries(spec);
  const systems = q.data ?? [];
  const used = new Set<Level>(systems.flatMap((s) => s.strip));
  const bucketMs = systems.find((s) => s.stripBucketMs)?.stripBucketMs;
  const startLabel = live ? `${spec.preset} ago` : dayMonHm(spec.from);
  return (
    <>
      <div className="page-h">
        <h1>Fleet</h1>
        {systems.length > 0 && (
          <>
            <span className="aside">
              {systems.length} {systems.length === 1 ? "system" : "systems"}, worst first
            </span>
            <StripLegend used={used} bucketMs={bucketMs} />
          </>
        )}
      </div>
      <QueryView q={q} what="systems">
        {(d) =>
          d.length === 0 ? (
            <Empty>No systems.</Empty>
          ) : (
            <ul className="fleet-grid" aria-label="Systems">
              {worstFirst(d).map((s) => (
                <FleetCard key={s.id} s={s} live={live} startLabel={startLabel} now={now} />
              ))}
            </ul>
          )
        }
      </QueryView>
    </>
  );
}

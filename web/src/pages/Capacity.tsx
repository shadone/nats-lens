// Capacity: what each stream keeps and what bounds it, and how full each
// server's JetStream file store is and when it fills, in the range's newest
// collection, with growth over the whole range.
import { useCapacity } from "../api/queries";
import { ServerCards } from "../components/CapacityServers";
import { StreamsTable } from "../components/CapacityStreams";
import { Empty, QueryView } from "../components/common";
import { CAPACITY_SUBTITLE, headerLine } from "../lib/capacityView";
import { hms } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";

export function Capacity() {
  usePageTitle("Capacity");
  const { spec } = useTimeRange();
  const system = useSystem();
  const q = useCapacity(spec);
  const d = q.data;
  return (
    <>
      <div className="page-h">
        <span className="aside">
          {CAPACITY_SUBTITLE}{" "}
          {d?.collected && d.epochAt !== undefined ? (
            <>
              {headerLine(d.streamsTotal, d.servers.length, d.growthFrom)}, in the collection of <span className="mono">{hms(d.epochAt)} UTC</span>.
            </>
          ) : (
            "In the newest collection of the range."
          )}
        </span>
      </div>
      <QueryView q={q} what="capacity">
        {(r) =>
          r.collected ? (
            <>
              <ServerCards servers={r.servers} withoutJetstream={r.serversWithoutJetstream} />
              <StreamsTable key={system} d={r} />
            </>
          ) : (
            <Empty>No collection in this range.</Empty>
          )
        }
      </QueryView>
    </>
  );
}

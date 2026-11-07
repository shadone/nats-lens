import { HistoryLink, useGrowthHistoryNote } from "../components/HistoryNote";
// Capacity: what each stream keeps and what bounds it, and how full each
// server's JetStream file store is and when it fills, in the range's newest
// collection, with growth over the whole range.
import { useCapacity } from "../api/queries";
import { ServerCards } from "../components/CapacityServers";
import { StreamsTable } from "../components/CapacityStreams";
import { Empty, QueryView } from "../components/common";
import { CAPACITY_SUBTITLE, hasRate, headerLine } from "../lib/capacityView";
import { hms, isoDate } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { useTimeRange } from "../lib/useTimeRange";

export function Capacity() {
  usePageTitle("Capacity");
  const { spec } = useTimeRange();
  const system = useSystem();
  const q = useCapacity(spec);
  const d = q.data;
  const missing = useGrowthHistoryNote();
  return (
    <>
      <div className="page-h">
        <span className="aside">
          {CAPACITY_SUBTITLE}{" "}
          {d?.collected && d.epochAt !== undefined ? (
            <>
              {!hasRate(d) && !missing ? <HistoryLink>{headerLine(d)}</HistoryLink> : headerLine(d, missing)}, in the collection of <span className="mono">{hms(d.epochAt)} UTC</span>.
            </>
          ) : (
            "In the newest collection of the range."
          )}
        </span>
      </div>
      {d?.collected && <p className="dh-note">
        Growth needs observations spanning 6 h. Ranges of at least 6 h read up to two collection intervals before the range (at most 5 min), and exclude its end.
        {d.growthFrom !== undefined && d.epochAt !== undefined && <> Collection bounds: {isoDate(d.growthFrom)} {hms(d.growthFrom)} to {isoDate(d.epochAt)} {hms(d.epochAt)} UTC; entity spans may be shorter.</>}
        {missing && (d.servers.some((s) => s.reported && s.perDay === undefined) || d.streams.some((s) => s.bytesPerDay === undefined)) && <> {missing}. <HistoryLink /></>}
      </p>}
      <QueryView q={q} what="capacity">
        {(r) =>
          r.collected ? (
            <>
              <ServerCards servers={r.servers} withoutJetstream={r.serversWithoutJetstream} />
              <StreamsTable key={system} d={r} answeredAt={q.dataUpdatedAt} live={spec.kind === "live"} />
            </>
          ) : (
            <Empty>No collection in this range.</Empty>
          )
        }
      </QueryView>
    </>
  );
}

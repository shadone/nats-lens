// The Servers page's charts across all servers: CPU and memory as a band
// with the median and the busiest server, total connections, and messages
// in and out with out dashed. They are the Overview's trend charts: the y
// axis starts at zero, except memory's, which spans its data.
import type { RangeSpec } from "../lib/timeRange";
import { Band, InOut, OneLine } from "./Trends";

export function FleetCharts({ spec, start, end }: { spec: RangeSpec; start: number; end: number }) {
  const f = { spec, start, end };
  return (
    <section className="srv-charts" aria-labelledby="sc-title">
      <h2 id="sc-title" className="srv-h2">
        All servers
      </h2>
      <div className="trends">
        <Band f={f} metric="cluster.cpu_percent" title="CPU" />
        <Band f={f} metric="cluster.mem_bytes" title="Memory" floor="data" />
        <OneLine f={f} metric="cluster.connections" title="Connections" label="all servers" />
        <InOut f={f} title="Messages" inMetric="cluster.in_msgs_rate" outMetric="cluster.out_msgs_rate" />
      </div>
    </section>
  );
}

// The Servers page's charts across all servers: CPU and memory as a band
// with the median and the busiest server, total connections, and messages
// in and out with out dashed. They are the Overview's trend charts: the y
// axis starts at zero, except memory's, which spans its data.
import { useChartGaps } from "../api/historyNotes";
import { SERVERS_QUERIES, useServersTrends } from "../api/servers";
import { pickTrend } from "../api/trendBatch";
import type { RangeSpec } from "../lib/timeRange";
import { useReportStale } from "./StaleContext";
import { Band, InOut, OneLine } from "./Trends";

export function FleetCharts({ spec, start, end }: { spec: RangeSpec; start: number; end: number }) {
  const f = { spec, start, end, gaps: useChartGaps(spec) };
  const b = useServersTrends(spec);
  useReportStale(b);
  const t = (metric: string) => pickTrend(b, SERVERS_QUERIES, { metric });
  return (
    <section className="srv-charts" aria-labelledby="sc-title">
      <h2 id="sc-title" className="srv-h2">
        All servers
      </h2>
      <div className="trends">
        <Band f={f} q={t("cluster.cpu_percent")} title="CPU" />
        <Band f={f} q={t("cluster.mem_bytes")} title="Memory" floor="data" />
        <OneLine f={f} q={t("cluster.connections")} title="Connections" label="all servers" />
        <InOut f={f} title="Messages" qi={t("cluster.in_msgs_rate")} qo={t("cluster.out_msgs_rate")} />
      </div>
    </section>
  );
}

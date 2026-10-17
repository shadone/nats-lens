// The Servers page's charts across all servers: CPU and memory as a band
// with the median and the busiest server, total connections, and messages
// in and out with out dashed.
import type { BandSeries } from "../api/servers";
import { useBandSeries } from "../api/servers";
import { errorText } from "./common";
import { fmtBytes, fmtCount, fmtInt } from "../lib/format";
import type { RangeSpec } from "../lib/timeRange";
import { FleetChart } from "./FleetChart";
import { useReportStale } from "./StaleContext";

const pct = (v: number) => `${fmtCount(v)}%`;

function last(s: BandSeries | undefined) {
  const p = s?.points ?? [];
  return p.length ? p[p.length - 1] : undefined;
}

function BandChart({ spec, metric, title, fmt, start, end }: { spec: RangeSpec; metric: string; title: string; fmt: (v: number) => string; start: number; end: number }) {
  const q = useBandSeries(spec, metric);
  useReportStale(q);
  const s = q.data;
  const p = last(s);
  // The median, then the highest server; the legend names the median.
  const now = p ? (p.high !== undefined ? `${fmt(p.value)}, max ${fmt(p.high)}` : fmt(p.value)) : undefined;
  return (
    <FleetChart
      title={title}
      now={now}
      band={s?.points}
      lines={[
        { points: s?.topPoints ?? [], mark: "busiest" },
        { points: s?.points ?? [], mark: "median" },
      ]}
      legend={[
        { mark: "band", label: "lowest to highest" },
        { mark: "median", label: "median" },
        { mark: "busiest", label: s?.top ? `busiest, ${s.top}` : "busiest" },
      ]}
      start={start}
      end={end}
      loading={q.isPending}
      error={q.error ? errorText(q.error) : undefined}
    />
  );
}

function ConnectionsChart({ spec, start, end }: { spec: RangeSpec; start: number; end: number }) {
  const q = useBandSeries(spec, "cluster.connections");
  useReportStale(q);
  const p = last(q.data);
  return (
    <FleetChart
      title="Connections"
      now={p ? fmtInt(p.value) : undefined}
      lines={[{ points: q.data?.points ?? [], mark: "line" }]}
      legend={[{ mark: "line", label: "all servers" }]}
      start={start}
      end={end}
      loading={q.isPending}
      error={q.error ? errorText(q.error) : undefined}
    />
  );
}

function MessagesChart({ spec, start, end }: { spec: RangeSpec; start: number; end: number }) {
  const qi = useBandSeries(spec, "cluster.in_msgs_rate");
  const qo = useBandSeries(spec, "cluster.out_msgs_rate");
  useReportStale(qi);
  useReportStale(qo);
  const i = last(qi.data);
  const o = last(qo.data);
  const now = i || o ? `${i ? fmtCount(i.value) : "–"} / ${o ? fmtCount(o.value) : "–"} /s` : undefined;
  const err = qi.error ?? qo.error;
  return (
    <FleetChart
      title="Messages in / out"
      now={now}
      lines={[
        { points: qo.data?.points ?? [], mark: "dashed" },
        { points: qi.data?.points ?? [], mark: "line" },
      ]}
      legend={[
        { mark: "line", label: "in" },
        { mark: "dashed", label: "out" },
      ]}
      start={start}
      end={end}
      loading={qi.isPending || qo.isPending}
      error={err ? errorText(err) : undefined}
    />
  );
}

export function FleetCharts({ spec, start, end }: { spec: RangeSpec; start: number; end: number }) {
  return (
    <section className="card sc-card" aria-labelledby="sc-title">
      <h2 id="sc-title" className="srv-h2">
        All servers
      </h2>
      <div className="sc-grid">
        <BandChart spec={spec} metric="cluster.cpu_percent" title="CPU" fmt={pct} start={start} end={end} />
        <BandChart spec={spec} metric="cluster.mem_bytes" title="Memory" fmt={fmtBytes} start={start} end={end} />
        <ConnectionsChart spec={spec} start={start} end={end} />
        <MessagesChart spec={spec} start={start} end={end} />
      </div>
    </section>
  );
}

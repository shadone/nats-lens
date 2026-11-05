// The five metric boxes of a server's page: CPU, memory, connections,
// messages in and out, slow consumers.
import { useChartGaps } from "../api/historyNotes";
import type { Level } from "../api/model";
import { useBandSeries, type ServerInfo } from "../api/servers";
import { fmtBytes, fmtCount, fmtInt } from "../lib/format";
import type { RangeSpec } from "../lib/timeRange";
import { errorText } from "./common";
import { MetricBox } from "./MetricBox";
import { useReportStale } from "./StaleContext";

/** CPU at or above this percentage of one core is high. */
export const CPU_HIGH_PCT = 60;
/** Connections at or above this share of the limit are near it. */
export const CONNECTIONS_NEAR = 0.8;

const pct = (v: number) => `${fmtCount(v)}%`;
const rate = (v: number) => `${fmtCount(v)}/s`;

function useTileSeries(spec: RangeSpec, metric: string, name: string) {
  const q = useBandSeries(spec, metric, name);
  useReportStale(q);
  return { series: q.data, loading: q.isPending, error: q.error ? errorText(q.error) : undefined };
}

function newest(points: { value: number }[] | undefined): number | undefined {
  return points?.length ? points[points.length - 1].value : undefined;
}

export function ServerTiles({ info, spec, start, end }: { info: ServerInfo; spec: RangeSpec; start: number; end: number }) {
  const s = info.server!;
  const name = s.name;
  const cpu = useTileSeries(spec, "server.cpu_percent", name);
  const mem = useTileSeries(spec, "server.mem_bytes", name);
  const conns = useTileSeries(spec, "server.connections", name);
  const msgIn = useTileSeries(spec, "server.in_msgs_rate", name);
  const msgOut = useTileSeries(spec, "server.out_msgs_rate", name);
  const slow = useTileSeries(spec, "server.slow_consumers", name);
  // A server that did not answer has no current value: its last numbers are
  // history, shown on the chart, not now.
  const down = !s.present;
  const downNote = down ? "did not answer in the newest collection" : undefined;
  const cpuNow = down ? null : (s.cpuPercent ?? null);
  const connNow = down ? null : (s.connections ?? null);
  const limit = info.limits?.maxConnections;
  const connStatus: Level = connNow !== null && limit ? (connNow >= limit * CONNECTIONS_NEAR ? "warning" : "ok") : "ok";
  const inNow = down ? null : (newest(msgIn.series?.points) ?? null);
  const outNow = down ? null : (newest(msgOut.series?.points) ?? null);
  const slowNow = down ? null : (newest(slow.series?.points) ?? null);
  const slowRange = s.slowConsumersInRange;
  const gaps = useChartGaps(spec);
  return (
    <div className="sd-tiles">
      <MetricBox
        title="CPU"
        description="CPU use of the server process, percent of one core; averaged per bucket."
        {...cpu}
        value={cpuNow}
        fmt={pct}
        status={cpuNow !== null && cpuNow >= CPU_HIGH_PCT ? "warning" : "ok"}
        stateWord="high"
        note={downNote}
        start={start}
        end={end}
        gaps={gaps}
      />
      <MetricBox
        title="Memory"
        description="Resident memory of the server process; averaged per bucket."
        {...mem}
        value={down ? null : (s.memBytes ?? null)}
        fmt={fmtBytes}
        note={downNote}
        start={start}
        end={end}
        gaps={gaps}
      />
      <MetricBox
        title="Connections"
        description="Client connections on the server; the last value of each bucket."
        {...conns}
        value={connNow}
        fmt={(v) => fmtInt(v)}
        status={connStatus}
        stateWord="near limit"
        note={downNote ?? (limit !== undefined ? `limit ${fmtInt(limit)}` : undefined)}
        start={start}
        end={end}
        gaps={gaps}
      />
      <MetricBox
        title="Messages"
        description="Messages the server received (in, solid line) and sent (out, dashed line) per second; averaged per bucket. The value and the track are for in; the note gives out."
        series={msgIn.series}
        series2={msgOut.series}
        loading={msgIn.loading}
        error={msgIn.error}
        value={inNow}
        fmt={rate}
        note={downNote ?? (outNow !== null ? `out ${rate(outNow)}, dashed` : undefined)}
        start={start}
        end={end}
        gaps={gaps}
      />
      <MetricBox
        title="Slow consumers"
        description="Clients the server dropped as slow consumers: the increase of its counter per bucket, per process. A restart's first collection adds nothing."
        {...slow}
        value={slowNow}
        fmt={(v) => fmtInt(v)}
        status={(slowRange ?? 0) > 0 ? "warning" : "ok"}
        stateWord={slowRange ? `${fmtInt(slowRange)} in range` : undefined}
        note={downNote ?? (slowRange === undefined ? undefined : slowRange === 0 ? "none in range" : "per bucket on the chart")}
        start={start}
        end={end}
        gaps={gaps}
      />
    </div>
  );
}

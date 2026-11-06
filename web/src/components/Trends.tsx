// The Overview's trends row: eight charts over the range on one time axis.
// CPU and memory are a band from the lowest to the highest server, with
// the median and the busiest server as lines; assets are stacked by
// replica count. The Servers page draws its charts across servers with the
// same OneLine, InOut and Band.
import type { Point, Unit } from "../api/model";
import type { TrendSeries } from "../api/overview";
import { pickTrend, useTrends, type TrendQuery, type TrendSlice } from "../api/trendBatch";
import { bucketLabel, type RangeSpec } from "../lib/timeRange";
import { bandEdges, bandText, inOutText, lastValue, minMaxText, stack, total } from "../lib/trends";
import { fmtValue } from "../lib/units";
import { errorText } from "./common";
import { useReportStale } from "./StaleContext";
import { TrendChart, type Layer } from "./TrendChart";

export interface Frame {
  spec: RangeSpec;
  /** Overview supplies destinations; other callers retain their existing headers. */
  chartLinks?: boolean;
  start: number;
  end: number;
}

function state(...qs: TrendSlice[]) {
  const err = qs.find((q) => q.error && !q.data)?.error;
  return { loading: qs.some((q) => q.isPending), error: err ? errorText(err) : undefined };
}

function fmt(v: number | undefined, unit: Unit): string | undefined {
  return v === undefined ? undefined : fmtValue(v, unit);
}

function pts(s: TrendSeries | undefined): Point[] {
  return s?.points ?? [];
}

export function OneLine({ f, q, title, label }: { f: Frame; q: TrendSlice; title: string; label: string }) {
  const s = q.data;
  const unit = s?.unit ?? "count";
  return (
    <TrendChart
      title={title}
      fmt={v => fmtValue(v, unit)}
      to={f.chartLinks ? ({ Servers: "/servers", Connections: "/connections", Messages: "/servers", Bytes: "/servers", CPU: "/servers", Memory: "/servers" } as Record<string, string>)[title] : undefined}
      now={fmt(lastValue(s?.points), unit)}
      bucketNote={unit === "count" ? "Last count in bucket" : "Mean value in bucket"}
      summary={minMaxText(s, unit)}
      start={f.start}
      end={f.end}
      stepMs={s?.stepMs}
      layers={[{ kind: "line", label, style: { tone: "accent" }, points: pts(s) }]}
      {...state(q)}
    />
  );
}

function SlowConsumers({ f, q }: { f: Frame; q: TrendSlice }) {
  const s = q.data;
  const sum = total(s?.points);
  return (
    <TrendChart
      fmt={v => `${fmtValue(v, "count")} events`}
      bucketNote="Total events in bucket"
      title="Slow consumers"
      to={f.chartLinks ? "/connections" : undefined}
      now={sum === undefined ? undefined : `${fmtValue(sum, "count")} in range`}
      start={f.start}
      end={f.end}
      stepMs={s?.stepMs}
      layers={[{ kind: "bars", label: s?.stepMs ? `events per ${bucketLabel(s.stepMs)}` : "events", style: { tone: "warn" }, points: pts(s) }]}
      {...state(q)}
    />
  );
}

export function InOut({ f, title, qi, qo }: { f: Frame; title: string; qi: TrendSlice; qo: TrendSlice }) {
  const unit = qi.data?.unit ?? qo.data?.unit ?? "per_second";
  const max = Math.max(qi.data?.max ?? -Infinity, qo.data?.max ?? -Infinity);
  return (
    <TrendChart
      title={title}
      fmt={v => fmtValue(v, unit)}
      to={f.chartLinks ? ({ Servers: "/servers", Connections: "/connections", Messages: "/servers", Bytes: "/servers", CPU: "/servers", Memory: "/servers" } as Record<string, string>)[title] : undefined}
      bucketNote="Mean rates in bucket"
      now={inOutText(lastValue(qi.data?.points), lastValue(qo.data?.points), unit)}
      summary={Number.isFinite(max) ? `max ${fmtValue(max, unit)}` : undefined}
      start={f.start}
      end={f.end}
      stepMs={qi.data?.stepMs ?? qo.data?.stepMs}
      layers={[
        { kind: "line", label: "in", style: { tone: "accent" }, points: pts(qi.data) },
        { kind: "line", label: "out", style: { tone: "muted", dash: true }, points: pts(qo.data) },
      ]}
      {...state(qi, qo)}
    />
  );
}

export function Band({ f, q, title, floor = "zero" }: { f: Frame; q: TrendSlice; title: string; floor?: "zero" | "data" }) {
  const s = q.data;
  const unit = s?.unit ?? "count";
  const text = bandText(s);
  const layers: Layer[] = [
    { kind: "band", label: "lowest to highest server", style: { tone: "accent" }, points: bandEdges(s) },
    { kind: "line", label: "median", style: { tone: "accent" }, points: pts(s) },
  ];
  if (text.busiest && s) layers.push({ kind: "line", label: text.busiest, style: { tone: "info", dash: true }, points: s.topPoints });
  return (
    <TrendChart
      title={title}
      fmt={v => fmtValue(v, unit)}
      to={f.chartLinks ? ({ Servers: "/servers", Connections: "/connections", Messages: "/servers", Bytes: "/servers", CPU: "/servers", Memory: "/servers" } as Record<string, string>)[title] : undefined}
      bucketNote="Mean median and busiest-server values; bounds are observed server extremes"
      now={text.now}
      summary={minMaxText(s, unit)}
      start={f.start}
      end={f.end}
      stepMs={s?.stepMs}
      layers={layers}
      floor={floor}
      {...state(q)}
    />
  );
}

function Assets({ f, qr, qs }: { f: Frame; qr: TrendSlice; qs: TrendSlice }) {
  const st = stack(pts(qr.data), pts(qs.data));
  const r = lastValue(qr.data?.points);
  const one = lastValue(qs.data?.points);
  const top = st.upper.length ? Math.max(...st.upper.map((p) => p.high)) : undefined;
  return (
    <TrendChart
      fmt={v => fmtValue(v, "count")}
      bucketNote="Last counts in bucket; stacked components shown separately"
      title="Assets"
      to={f.chartLinks ? "/placement" : undefined}
      now={r === undefined && one === undefined ? undefined : `${fmt(r, "count") ?? "–"} replicated, ${fmt(one, "count") ?? "–"} single`}
      summary={top === undefined ? undefined : `max ${fmtValue(top, "count")} in all`}
      start={f.start}
      end={f.end}
      stepMs={qr.data?.stepMs}
      layers={[
        { kind: "band", stacked: true, label: "replicated", style: { tone: "accent", edge: true }, points: st.lower },
        { kind: "band", stacked: true, label: "one replica", style: { tone: "warn", edge: true }, points: st.upper },
      ]}
      {...state(qr, qs)}
    />
  );
}

/** The series of the trends row, read in one batch. */
export const TREND_QUERIES: TrendQuery[] = [
  "cluster.servers_up",
  "cluster.connections",
  "cluster.slow_consumers",
  "cluster.in_msgs_rate",
  "cluster.out_msgs_rate",
  "cluster.in_bytes_rate",
  "cluster.out_bytes_rate",
  "cluster.cpu_percent",
  "cluster.mem_bytes",
  "cluster.assets_replicated",
  "cluster.assets_single",
].map((metric) => ({ metric }));

export function Trends({ spec, start, end }: Frame) {
  const f = { spec, start, end, chartLinks: true };
  const b = useTrends(spec, TREND_QUERIES);
  useReportStale(b);
  const t = (metric: string) => pickTrend(b, TREND_QUERIES, { metric });
  return (
    <div className="trends">
      <OneLine f={f} q={t("cluster.servers_up")} title="Servers" label="answering" />
      <OneLine f={f} q={t("cluster.connections")} title="Connections" label="open" />
      <SlowConsumers f={f} q={t("cluster.slow_consumers")} />
      <InOut f={f} title="Messages" qi={t("cluster.in_msgs_rate")} qo={t("cluster.out_msgs_rate")} />
      <InOut f={f} title="Bytes" qi={t("cluster.in_bytes_rate")} qo={t("cluster.out_bytes_rate")} />
      <Band f={f} q={t("cluster.cpu_percent")} title="CPU" />
      <Band f={f} q={t("cluster.mem_bytes")} title="Memory" floor="data" />
      <Assets f={f} qr={t("cluster.assets_replicated")} qs={t("cluster.assets_single")} />
    </div>
  );
}

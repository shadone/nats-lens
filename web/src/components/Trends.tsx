// The Overview's trends row: eight charts over the range on one time axis.
// CPU and memory are a band from the lowest to the highest server, with
// the median and the busiest server as lines; assets are stacked by
// replica count. The Servers page draws its charts across servers with the
// same OneLine, InOut and Band.
import type { Point, Unit } from "../api/model";
import { useTrend, type TrendSeries } from "../api/overview";
import { bucketLabel, type RangeSpec } from "../lib/timeRange";
import { bandEdges, bandText, inOutText, lastValue, minMaxText, stack, total } from "../lib/trends";
import { fmtValue } from "../lib/units";
import { errorText } from "./common";
import { useReportStale } from "./StaleContext";
import { TrendChart, type Layer } from "./TrendChart";

export interface Frame {
  spec: RangeSpec;
  start: number;
  end: number;
}

type Q = ReturnType<typeof useTrend>;

function state(...qs: Q[]) {
  const err = qs.find((q) => q.error && !q.data)?.error;
  return { loading: qs.some((q) => q.isPending), error: err ? errorText(err) : undefined };
}

function fmt(v: number | undefined, unit: Unit): string | undefined {
  return v === undefined ? undefined : fmtValue(v, unit);
}

function pts(s: TrendSeries | undefined): Point[] {
  return s?.points ?? [];
}

export function OneLine({ f, metric, title, label }: { f: Frame; metric: string; title: string; label: string }) {
  const q = useTrend(f.spec, metric);
  useReportStale(q);
  const s = q.data;
  const unit = s?.unit ?? "count";
  return (
    <TrendChart
      title={title}
      now={fmt(lastValue(s?.points), unit)}
      summary={minMaxText(s, unit)}
      start={f.start}
      end={f.end}
      stepMs={s?.stepMs}
      layers={[{ kind: "line", label, style: { tone: "accent" }, points: pts(s) }]}
      {...state(q)}
    />
  );
}

function SlowConsumers({ f }: { f: Frame }) {
  const q = useTrend(f.spec, "cluster.slow_consumers");
  useReportStale(q);
  const s = q.data;
  const sum = total(s?.points);
  return (
    <TrendChart
      title="Slow consumers"
      now={sum === undefined ? undefined : `${fmtValue(sum, "count")} in range`}
      start={f.start}
      end={f.end}
      stepMs={s?.stepMs}
      layers={[{ kind: "bars", label: s?.stepMs ? `events per ${bucketLabel(s.stepMs)}` : "events", style: { tone: "warn" }, points: pts(s) }]}
      {...state(q)}
    />
  );
}

export function InOut({ f, title, inMetric, outMetric }: { f: Frame; title: string; inMetric: string; outMetric: string }) {
  const qi = useTrend(f.spec, inMetric);
  const qo = useTrend(f.spec, outMetric);
  useReportStale(qi);
  useReportStale(qo);
  const unit = qi.data?.unit ?? qo.data?.unit ?? "per_second";
  const max = Math.max(qi.data?.max ?? -Infinity, qo.data?.max ?? -Infinity);
  return (
    <TrendChart
      title={title}
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

export function Band({ f, metric, title }: { f: Frame; metric: string; title: string }) {
  const q = useTrend(f.spec, metric);
  useReportStale(q);
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
      now={text.now}
      summary={minMaxText(s, unit)}
      start={f.start}
      end={f.end}
      stepMs={s?.stepMs}
      layers={layers}
      {...state(q)}
    />
  );
}

function Assets({ f }: { f: Frame }) {
  const qr = useTrend(f.spec, "cluster.assets_replicated");
  const qs = useTrend(f.spec, "cluster.assets_single");
  useReportStale(qr);
  useReportStale(qs);
  const st = stack(pts(qr.data), pts(qs.data));
  const r = lastValue(qr.data?.points);
  const one = lastValue(qs.data?.points);
  const top = st.upper.length ? Math.max(...st.upper.map((p) => p.high)) : undefined;
  return (
    <TrendChart
      title="Assets"
      now={r === undefined && one === undefined ? undefined : `${fmt(r, "count") ?? "–"} replicated, ${fmt(one, "count") ?? "–"} single`}
      summary={top === undefined ? undefined : `max ${fmtValue(top, "count")} in all`}
      start={f.start}
      end={f.end}
      stepMs={qr.data?.stepMs}
      layers={[
        { kind: "band", label: "replicated", style: { tone: "accent", edge: true }, points: st.lower },
        { kind: "band", label: "one replica", style: { tone: "warn", edge: true }, points: st.upper },
      ]}
      {...state(qr, qs)}
    />
  );
}

export function Trends({ spec, start, end }: Frame) {
  const f = { spec, start, end };
  return (
    <div className="trends">
      <OneLine f={f} metric="cluster.servers_up" title="Servers" label="answering" />
      <OneLine f={f} metric="cluster.connections" title="Connections" label="open" />
      <SlowConsumers f={f} />
      <InOut f={f} title="Messages" inMetric="cluster.in_msgs_rate" outMetric="cluster.out_msgs_rate" />
      <InOut f={f} title="Bytes" inMetric="cluster.in_bytes_rate" outMetric="cluster.out_bytes_rate" />
      <Band f={f} metric="cluster.cpu_percent" title="CPU" />
      <Band f={f} metric="cluster.mem_bytes" title="Memory" />
      <Assets f={f} />
    </div>
  );
}

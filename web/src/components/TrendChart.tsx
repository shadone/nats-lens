// A small chart over the range with any mix of layers: lines (solid or
// dashed), filled bands between two edges (a min-max band, or one layer of
// a stacked area) and bars. The y axis starts at zero. A gap in the data
// of more than two steps breaks the line, since a bucket without data has
// no point. The legend draws each layer's own mark.
import type { Point } from "../api/model";
import { chartName, rangeText } from "../charts/chartLabel";
import { dayMonHm } from "../lib/format";
import type { EdgePoint } from "../lib/trends";
import { Legend, fillClass, strokeClass, type LegendItem, type MarkStyle } from "./Legend";

const W = 300;
const H = 70;

export type Layer =
  | { kind: "line"; label: string; style: MarkStyle; points: Point[] }
  | { kind: "band"; label: string; style: MarkStyle; points: EdgePoint[] }
  | { kind: "bars"; label: string; style: MarkStyle; points: Point[] };

interface Props {
  title: string;
  /** The current value as text; absent when nothing was observed. */
  now?: string;
  /** What the chart's range was, for the footer and the accessible name: "min 14, max 20". */
  summary?: string;
  start: number;
  end: number;
  /** Width of one bucket, to tell a gap from the next point. */
  stepMs?: number;
  layers: Layer[];
  loading?: boolean;
  error?: string;
}

/** Runs of points without a gap of more than two steps between neighbours. */
export function segments<T extends { time: number }>(pts: T[], stepMs: number): T[][] {
  const out: T[][] = [];
  for (const p of pts) {
    const run = out[out.length - 1];
    if (run && (stepMs <= 0 || p.time - run[run.length - 1].time <= 2 * stepMs)) run.push(p);
    else out.push([p]);
  }
  return out;
}

function values(l: Layer): number[] {
  return l.kind === "band" ? l.points.flatMap((p) => [p.low, p.high]) : l.points.map((p) => p.value);
}

export function TrendChart({ title, now, summary, start, end, stepMs = 0, layers, loading, error }: Props) {
  const all = layers.flatMap(values);
  const empty = all.length === 0;
  const lo = empty ? 0 : Math.min(0, ...all);
  let hi = empty ? 1 : Math.max(...all);
  if (hi === lo) hi = lo + 1;
  const span = end - start || 1;
  const x = (t: number) => (((t - start) / span) * W).toFixed(1);
  const y = (v: number) => (H - 2 - ((v - lo) / (hi - lo)) * (H - 8)).toFixed(1);
  const barW = Math.max(1, stepMs > 0 ? (stepMs / span) * W * 0.7 : 2);
  const legend: LegendItem[] = layers.map((l) => ({ label: l.label, kind: l.kind, style: l.style }));
  const shown = loading && empty ? "…" : (now ?? "–");
  return (
    <div className="card tc">
      <div className="tc-h">
        <span className="lbl">{title}</span>
        <b className="mono">{shown}</b>
      </div>
      {error && empty ? (
        <p className="muted-note">Could not load: {error}</p>
      ) : (
        <svg className="tc-svg" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={chartName(title, now, rangeText(start, end), summary)}>
          <line x1="0" x2={W} y1={H - 1} y2={H - 1} className="tc-axis" vectorEffect="non-scaling-stroke" />
          {layers.map((l, li) => {
            if (l.kind === "band") {
              return segments(l.points, stepMs).map((run, ri) => {
                // One point between two gaps has no width to fill: a thin
                // vertical mark from low to high stands for it.
                if (run.length === 1) {
                  const p = run[0];
                  return <line key={`${li}-${ri}`} x1={x(p.time)} x2={x(p.time)} y1={y(p.low)} y2={y(p.high)} className={`tc-band-pt tk-${l.style.tone}`} vectorEffect="non-scaling-stroke" />;
                }
                const top = run.map((p) => `${x(p.time)},${y(p.high)}`);
                const bottom = [...run].reverse().map((p) => `${x(p.time)},${y(p.low)}`);
                return (
                  <g key={`${li}-${ri}`}>
                    <polygon points={top.concat(bottom).join(" ")} className={fillClass("band", l.style)} />
                    {l.style.edge && <polyline points={top.join(" ")} className={strokeClass(l.style)} vectorEffect="non-scaling-stroke" />}
                  </g>
                );
              });
            }
            if (l.kind === "bars") {
              return l.points
                .filter((p) => p.value > 0)
                .map((p) => (
                  <rect key={`${li}-${p.time}`} x={(Number(x(p.time)) - barW / 2).toFixed(1)} y={y(p.value)} width={barW.toFixed(1)} height={(Number(y(lo)) - Number(y(p.value))).toFixed(1)} className={fillClass("bars", l.style)} />
                ));
            }
            return segments(l.points, stepMs).map((run, ri) =>
              run.length === 1 ? (
                <circle key={`${li}-${ri}`} cx={x(run[0].time)} cy={y(run[0].value)} r="1.6" className={`tc-dot tk-${l.style.tone}`} />
              ) : (
                <polyline key={`${li}-${ri}`} points={run.map((p) => `${x(p.time)},${y(p.value)}`).join(" ")} className={strokeClass(l.style)} vectorEffect="non-scaling-stroke" />
              ),
            );
          })}
        </svg>
      )}
      <div className="tc-f mono" aria-hidden="true">
        <span>{dayMonHm(start)}</span>
        <span>{dayMonHm(end)}</span>
      </div>
      <div className="tc-lg">
        <Legend items={legend} label={`${title} legend`} />
        {summary && <span className="tc-sum mono">{summary}</span>}
      </div>
    </div>
  );
}

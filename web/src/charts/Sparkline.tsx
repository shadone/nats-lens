// The tile sparkline of the reference: area and line for the primary
// series, a dashed line for the second, shaded windows behind.
import { ChartInspection } from "./ChartInspection";
import { usePageHistory } from "../components/HistoryNote";
import type { ServerHistoryGap } from "../api/history";
import { ChartHistoryNotice, ChartHistoryStatus, HistoryGaps } from "./HistoryGaps";
import type { Level, Point, Window } from "../api/model";
import { chartName, rangeText } from "./chartLabel";
import { changedSegments, tickedMoves } from "../lib/tileChart";
import { isTrouble } from "../lib/view";

const W = 200;
const H = 40;

export function sparkColour(status: Level): string {
  return status === "critical" ? "var(--sev-critical)" : status === "warning" ? "var(--sev-warning)" : "var(--accent)";
}

interface Props {
  points: Point[];
  stepMs?: number;
  bucketNote?: string;
  fmt?: (value: number) => string;
  labels?: [string, string];
  points2?: Point[];
  status: Level;
  windows: Window[];
  start: number;
  end: number;
  label: string;
  /** The latest value with its unit, for the accessible name; absent when nothing was observed. */
  current?: string;
  /** Appended to the accessible name, for example "as of 14:30:05 UTC". */
  note?: string;
  /**
   * "status" draws the line in the status colour. "change" draws it neutral
   * and puts the accent only on the change: the segments where the value
   * changes, or, when most segments of a run change, a dot on the newest
   * point; a bucket whose low or high goes beyond the line around it
   * (`tickedMoves`) gets a tick between them. A warning or critical status keeps its colour.
   */
  tone?: "status" | "change";
}

/** x by time over [start, end], y scaled to the shared min and max. */
export function sparkPoints(a: Point[], start: number, end: number, mn: number, mx: number): string[] {
  const span = end - start || 1;
  return a.map((p) => {
    const x = ((p.time - start) / span) * W;
    const y = H - 3 - ((p.value - mn) / (mx - mn)) * (H - 8);
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });
}

/** Consecutive changed segments joined, each from the point before the change to the last changed point. */
function changeRuns(run: Point[]): Point[][] {
  const out: Point[][] = [];
  let prev = -2;
  for (const i of changedSegments(run)) {
    if (i === prev + 1) out[out.length - 1].push(run[i]);
    else out.push([run[i - 1], run[i]]);
    prev = i;
  }
  return out;
}

/** Whether a bucket's lowest or highest differs from its value. */
function hasRange(p: Point): boolean {
  return (p.low !== undefined && p.low !== p.value) || (p.high !== undefined && p.high !== p.value);
}

/** Whether more than half of a run's segments change, as a slow steady growth does. */
function mostlyChanging(run: Point[]): boolean {
  return run.length > 2 && changedSegments(run).length > (run.length - 1) / 2;
}

export function Sparkline({ stepMs, bucketNote, fmt = String, labels = ["value", "second series"], points, points2, status, windows, start, end, label, current, note, tone }: Props) {
  // The change tone also says a bucket's range, which its last value hides.
  const text = (p: Point) => tone === "change" && hasRange(p) ? `${fmt(p.value)} (${fmt(p.low ?? p.value)} to ${fmt(p.high ?? p.value)} in bucket)` : fmt(p.value);
  return <ChartInspection label={label} start={start} end={end} stepMs={stepMs} bucketNote={bucketNote} series={[
    { label: labels[0], points: points.map(p => ({ time: p.time, text: text(p) })) },
    ...(points2 ? [{ label: labels[1], points: points2.map(p => ({ time: p.time, text: fmt(p.value) })) }] : []),
  ]}><SparkDrawing points={points} points2={points2} status={status} windows={windows} start={start} end={end} label={label} current={current} note={note} tone={tone} /></ChartInspection>;
}

function SparkDrawing({ points, points2, status, windows, start, end, label, current, note, tone = "status" }: Props) {
  const history = usePageHistory();
  const gaps = history?.serverGaps ?? [];
  const period = rangeText(start, end);
  if (points.length === 0) {
    return (
      <><svg className="spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={chartName(label, undefined, period, note)}>
        <HistoryGaps start={start} end={end} width={W} height={H} />
        <line x1="0" x2={W} y1={H - 3} y2={H - 3} style={{ stroke: "var(--border)" }} strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
      </svg><ChartHistoryStatus start={start} end={end} /><ChartHistoryNotice start={start} end={end}/></>
    );
  }
  const ranges = tone === "change" ? tickedMoves(points) : [];
  const all = points.concat(points2 ?? []).map((p) => p.value).concat(ranges.flatMap((p) => [p.low ?? p.value, p.high ?? p.value]));
  let mn = Math.min(...all);
  let mx = Math.max(...all);
  // A series that stays at 0 is a bare line on the baseline, with no fill.
  const flatZero = mx === 0 && mn === 0;
  if (mx === mn) {
    // A series that stays at 0 lies on the baseline; another constant sits mid-height.
    mx = mn + 1;
    if (mn !== 0) mn = mn - 1;
  }
  const runs = splitServerPoints(points, gaps);
  const neutral = tone === "change" && !isTrouble(status);
  const col = neutral ? "var(--text-muted)" : sparkColour(status);
  const span = end - start || 1;
  return (
    <><svg className="spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={chartName(label, current, period, note)}>
      <HistoryGaps start={start} end={end} width={W} height={H} />
      {windows.map((w) => {
        const x0 = Math.max(0, ((w.start - start) / span) * W);
        const x1 = Math.min(W, ((w.end - start) / span) * W);
        return x1 > x0 ? <rect key={w.start} x={x0.toFixed(1)} y="0" width={(x1 - x0).toFixed(1)} height={H} style={{ fill: "var(--upgrade-fill)" }} /> : null;
      })}
      {!flatZero && runs.map((run, i) => {
        const p = sparkPoints(run, start, end, mn, mx);
        return <polygon key={i} points={`${p[0].split(",")[0]},${H} ${p.join(" ")} ${p[p.length-1].split(",")[0]},${H}`} style={{ fill: col, fillOpacity: neutral ? 0.07 : 0.1 }} />;
      })}
      {points2 && points2.length > 0 && (
        splitServerPoints(points2, gaps).map((run, i) => <polyline key={i}
          points={sparkPoints(run, start, end, mn, mx).join(" ")}
          style={{ fill: "none", stroke: "var(--text-muted)" }}
          strokeWidth="1.2"
          strokeDasharray="3 2"
          vectorEffect="non-scaling-stroke"
        />)
      )}
      {runs.map((run, i) => <polyline key={i} points={sparkPoints(run, start, end, mn, mx).join(" ")} style={{ fill: "none", stroke: col }} strokeWidth={neutral ? 1.3 : 1.6} strokeLinejoin="round" vectorEffect="non-scaling-stroke" />)}
      {neutral && runs.flatMap((run, i) => mostlyChanging(run) ? [] : changeRuns(run).map((seg, j) => <polyline key={`c${i}-${j}`} className="spark-change" points={sparkPoints(seg, start, end, mn, mx).join(" ")} style={{ fill: "none", stroke: "var(--accent)" }} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" vectorEffect="non-scaling-stroke" />))}
      {neutral && runs.length > 0 && mostlyChanging(runs[runs.length - 1]) && (() => {
        // A zero-length path with a round cap stays a round dot in the stretched viewBox.
        const [x, y] = sparkPoints([points[points.length - 1]], start, end, mn, mx)[0].split(",");
        return <path className="spark-now" d={`M${x},${y}h0`} style={{ stroke: "var(--accent)" }} strokeWidth="5" strokeLinecap="round" vectorEffect="non-scaling-stroke" />;
      })()}
      {ranges.map((p) => {
        const [[x, y0], [, y1]] = sparkPoints([{ time: p.time, value: p.low ?? p.value }, { time: p.time, value: p.high ?? p.value }], start, end, mn, mx).map((c) => c.split(","));
        return <line key={`r${p.time}`} className="spark-range" x1={x} x2={x} y1={y0} y2={y1} style={{ stroke: neutral ? "var(--accent)" : col }} strokeWidth="2" strokeLinecap="round" vectorEffect="non-scaling-stroke" />;
      })}
    </svg><ChartHistoryStatus start={start} end={end} /><ChartHistoryNotice start={start} end={end}/></>
  );
}

/** A bucket can hold data alongside a missing report. Keep the point but
 * never connect across recorded missing intervals. */
export function splitServerPoints(points: Point[], gaps: ServerHistoryGap[]): Point[][] {
  const runs: Point[][] = [];
  for (const p of points) {
    const run = runs[runs.length - 1];
    const prev = run?.[run.length - 1];
    if (!prev || gaps.some(g => g.start < p.time && g.end > prev.time)) runs.push([p]);
    else run.push(p);
  }
  return runs;
}

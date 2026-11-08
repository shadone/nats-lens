import type {InvestigationSelection} from "../lib/investigationContext";
// The streams card's small 7-day line, from the reference's g7.
import { ChartInspection } from "./ChartInspection";
import type { Level, Point } from "../api/model";
import { chartName } from "./chartLabel";

/** `period` names the time drawn, for example "last 7 days"; `fmt` formats the latest value with its unit. */
interface Props { bucketNote?:string; actionTarget?:HTMLElement|null; investigation?:InvestigationSelection; points: Point[]; level: Level; label: string; period: string; fmt: (v: number) => string; frame?: { start: number; end: number; stepMs?: number } }
export function MiniLine(p: Props) {
  if (!p.frame) return <MiniDrawing {...p} />;
  return <ChartInspection actionTarget={p.actionTarget} investigation={p.investigation} label={p.label} {...p.frame} bucketNote={p.bucketNote??"Mean value in bucket"} series={[{label:p.label,points:p.points.map(v=>({time:v.time,text:p.fmt(v.value)}))}]}><MiniDrawing {...p} /></ChartInspection>;
}
function MiniDrawing({ points, level, label, period, fmt, frame }: Props) {
  const W = 52;
  const H = 16;
  if (points.length < 2) {
    return (
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={chartName(label, points.length ? fmt(points[points.length - 1].value) : undefined, period)}>
        <line x1="0" x2={W} y1={H - 2} y2={H - 2} style={{ stroke: "var(--border)" }} strokeDasharray="2 2" />
      </svg>
    );
  }
  const vals = points.map((p) => p.value);
  const mn = Math.min(...vals);
  let mx = Math.max(...vals);
  if (mx === mn) mx = mn + 1;
  const col = level === "critical" ? "var(--sev-critical)" : level === "warning" ? "var(--sev-warning)" : "var(--text-muted)";
  const xy = (p: Point, i: number) => `${((frame ? (p.time - frame.start) / (frame.end - frame.start || 1) : i / (points.length - 1)) * W).toFixed(1)},${(H - 2 - ((p.value - mn) / (mx - mn)) * (H - 4)).toFixed(1)}`;
  const runs: { p: Point; i: number }[][] = [];
  points.forEach((p,i) => {
    const run = runs[runs.length-1];
    if (!run || (frame?.stepMs && p.time - run[run.length-1].p.time > 2 * frame.stepMs)) runs.push([{p,i}]);
    else run.push({p,i});
  });
  return (
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={chartName(label, fmt(points[points.length - 1].value), period)}>
      {runs.map((run,i)=><polyline key={i} points={run.map(({p,i})=>xy(p,i)).join(" ")} style={{ fill: "none", stroke: col }} strokeWidth="1.3" vectorEffect="non-scaling-stroke" />)}
    </svg>
  );
}

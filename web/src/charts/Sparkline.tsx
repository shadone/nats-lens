// The tile sparkline of the reference: area and line for the primary
// series, a dashed line for the second, shaded windows behind.
import type { Level, Point, Window } from "../api/model";
import { chartName, rangeText } from "./chartLabel";

const W = 200;
const H = 40;

export function sparkColour(status: Level): string {
  return status === "critical" ? "var(--sev-critical)" : status === "warning" ? "var(--sev-warning)" : "var(--accent)";
}

interface Props {
  points: Point[];
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

export function Sparkline({ points, points2, status, windows, start, end, label, current, note }: Props) {
  const period = rangeText(start, end);
  if (points.length === 0) {
    return (
      <svg className="spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={chartName(label, undefined, period, note)}>
        <line x1="0" x2={W} y1={H - 3} y2={H - 3} style={{ stroke: "var(--border)" }} strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
      </svg>
    );
  }
  const all = points.concat(points2 ?? []).map((p) => p.value);
  let mn = Math.min(...all);
  let mx = Math.max(...all);
  if (mx === mn) {
    mx = mn + 1;
    mn = mn - 1;
  }
  const p = sparkPoints(points, start, end, mn, mx);
  const first = p[0].split(",")[0];
  const last = p[p.length - 1].split(",")[0];
  const col = sparkColour(status);
  const span = end - start || 1;
  return (
    <svg className="spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={chartName(label, current, period, note)}>
      {windows.map((w) => {
        const x0 = Math.max(0, ((w.start - start) / span) * W);
        const x1 = Math.min(W, ((w.end - start) / span) * W);
        return x1 > x0 ? <rect key={w.start} x={x0.toFixed(1)} y="0" width={(x1 - x0).toFixed(1)} height={H} style={{ fill: "var(--upgrade-fill)" }} /> : null;
      })}
      <polygon points={`${first},${H} ${p.join(" ")} ${last},${H}`} style={{ fill: col, fillOpacity: 0.1 }} />
      {points2 && points2.length > 0 && (
        <polyline
          points={sparkPoints(points2, start, end, mn, mx).join(" ")}
          style={{ fill: "none", stroke: "var(--text-muted)" }}
          strokeWidth="1.2"
          strokeDasharray="3 2"
          vectorEffect="non-scaling-stroke"
        />
      )}
      <polyline points={p.join(" ")} style={{ fill: "none", stroke: col }} strokeWidth="1.6" strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

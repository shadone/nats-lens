// The streams card's small 7-day line, from the reference's g7.
import type { Level, Point } from "../api/model";
import { chartName } from "./chartLabel";

/** `period` names the time drawn, for example "last 7 days"; `fmt` formats the latest value with its unit. */
export function MiniLine({ points, level, label, period, fmt }: { points: Point[]; level: Level; label: string; period: string; fmt: (v: number) => string }) {
  const W = 52;
  const H = 16;
  if (points.length < 2) {
    return (
      <svg className="gr" viewBox={`0 0 ${W} ${H}`} role="img" aria-label={chartName(label, points.length ? fmt(points[points.length - 1].value) : undefined, period)}>
        <line x1="0" x2={W} y1={H - 2} y2={H - 2} style={{ stroke: "var(--border)" }} strokeDasharray="2 2" />
      </svg>
    );
  }
  const vals = points.map((p) => p.value);
  const mn = Math.min(...vals);
  let mx = Math.max(...vals);
  if (mx === mn) mx = mn + 1;
  const col = level === "critical" ? "var(--sev-critical)" : level === "warning" ? "var(--sev-warning)" : "var(--text-muted)";
  const pts = vals.map((v, i) => `${((i / (vals.length - 1)) * W).toFixed(1)},${(H - 2 - ((v - mn) / (mx - mn)) * (H - 4)).toFixed(1)}`);
  return (
    <svg className="gr" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={chartName(label, fmt(points[points.length - 1].value), period)}>
      <polyline points={pts.join(" ")} style={{ fill: "none", stroke: col }} strokeWidth="1.3" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

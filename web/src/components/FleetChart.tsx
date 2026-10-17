// A chart across all servers: an optional band from the lowest to the
// highest server, with lines on top (median, busiest server, a total, a
// dashed second total), and a legend that draws each mark.
import type { Point } from "../api/model";
import type { BandPoint } from "../api/servers";
import { chartName, rangeText } from "../charts/chartLabel";
import { dayMonHm } from "../lib/format";
import { SeriesLegend, type LegendItem, type Mark } from "./SeriesLegend";

const W = 300;
const H = 84;

export interface ChartLine {
  points: Point[];
  mark: Exclude<Mark, "band">;
}

interface Props {
  title: string;
  /** The current value as text, for example "5% median, 71% highest". */
  now?: string;
  band?: BandPoint[];
  lines: ChartLine[];
  legend: LegendItem[];
  start: number;
  end: number;
  loading?: boolean;
  error?: string;
}

export function FleetChart({ title, now, band = [], lines, legend, start, end, loading, error }: Props) {
  const banded = band.filter((p) => p.low !== undefined && p.high !== undefined);
  const values = [...banded.flatMap((p) => [p.low!, p.high!]), ...lines.flatMap((l) => l.points.map((p) => p.value))];
  const empty = values.length === 0;
  let lo = empty ? 0 : Math.min(...values);
  let hi = empty ? 1 : Math.max(...values);
  if (hi === lo) {
    hi += 1;
    lo -= 1;
  }
  const span = end - start || 1;
  const x = (t: number) => (((t - start) / span) * W).toFixed(1);
  const y = (v: number) => (H - 3 - ((v - lo) / (hi - lo)) * (H - 8)).toFixed(1);
  const poly = (pts: Point[]) => pts.map((p) => `${x(p.time)},${y(p.value)}`).join(" ");
  const bandPoly = banded.length
    ? banded.map((p) => `${x(p.time)},${y(p.high!)}`).concat([...banded].reverse().map((p) => `${x(p.time)},${y(p.low!)}`)).join(" ")
    : "";
  return (
    <div className="sc">
      <div className="sc-h">
        <span className="lbl">{title}</span>
        <b className="mono">{loading ? "…" : (now ?? "–")}</b>
      </div>
      {error && empty ? (
        <p className="muted-note">Could not load: {error}</p>
      ) : (
        <svg className="sc-svg" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={chartName(title, now, rangeText(start, end))}>
          <line x1="0" x2={W} y1={H - 1} y2={H - 1} className="sc-axis" vectorEffect="non-scaling-stroke" />
          {bandPoly && <polygon points={bandPoly} className="sc-band" />}
          {lines.map((l) => (l.points.length ? <polyline key={l.mark} points={poly(l.points)} className={`sc-${l.mark}`} vectorEffect="non-scaling-stroke" /> : null))}
        </svg>
      )}
      <div className="sc-f mono">
        <span>{dayMonHm(start)}</span>
        <span>{dayMonHm(end)}</span>
      </div>
      <SeriesLegend items={legend} />
    </div>
  );
}

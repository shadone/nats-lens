// A metric over the range on detail pages: the sparkline drawn larger, with
// the latest value, min and max as text and the range ends below. A series
// whose refresh failed stays on screen, dimmed and dated.
import type { Series, Window } from "../api/model";
import { useReportStale } from "../components/StaleContext";
import { errorText } from "../components/common";
import { dayMonHm, hms } from "../lib/format";
import { fmtValue } from "../lib/units";
import { Sparkline } from "./Sparkline";

interface Props {
  title: string;
  series?: Series;
  start: number;
  end: number;
  windows?: Window[];
  loading?: boolean;
  /** Why the last load failed. */
  error?: string;
  /** When the series was last loaded. */
  updatedAt?: number;
}

export function SeriesChart({ title, series, start, end, windows = [], loading, error, updatedAt }: Props) {
  const pts = series?.points ?? [];
  const latest = pts.length ? pts[pts.length - 1].value : undefined;
  const fmt = (v: number | undefined) => (v === undefined ? "–" : fmtValue(v, series?.unit ?? "count"));
  const stale = !!error && series !== undefined;
  const asOf = stale && updatedAt ? `as of ${hms(updatedAt)} UTC` : stale ? "from an earlier refresh" : undefined;
  return (
    <div className={`card chart${stale ? " stale" : ""}`}>
      <div className="chart-h">
        <span className="lbl">{title}</span>
        <b className="mono">{loading ? "…" : fmt(latest)}</b>
      </div>
      {error && !series ? (
        <p className="muted-note">Could not load: {error}</p>
      ) : (
        <Sparkline stepMs={series?.stepMs} fmt={v => fmtValue(v, series?.unit ?? "count")} points={pts} status="ok" windows={windows} start={start} end={end} label={title} current={latest === undefined ? undefined : fmt(latest)} note={asOf} />
      )}
      <div className="chart-f mono">
        <span>{dayMonHm(start)}</span>
        <span>
          min {fmt(series?.min)} · max {fmt(series?.max)}
        </span>
        <span>{dayMonHm(end)}</span>
      </div>
      {asOf && <div className="chart-stale">{asOf}</div>}
    </div>
  );
}

interface QueryLike {
  data: Series | undefined;
  error: unknown;
  isPending: boolean;
  dataUpdatedAt: number;
  refetch: () => unknown;
}

/** A chart for a series query; a failed refresh also joins the page's banner. */
export function QuerySeriesChart({ q, ...rest }: { q: QueryLike; title: string; start: number; end: number; windows?: Window[] }) {
  useReportStale(q);
  return <SeriesChart {...rest} series={q.data} loading={q.isPending} error={q.error ? errorText(q.error) : undefined} updatedAt={q.dataUpdatedAt} />;
}

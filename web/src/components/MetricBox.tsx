// A metric box: a title with an (i) description, the current value top
// right, a line chart unless the caller leaves it out, and a range track
// with the window's min on the left, its max on the right and a dot at the
// current value. A warning or critical status colours the value and the
// dot and shows a word; info only tints the dot. A configured limit belongs
// in the note, not on the track.
//
// The track's ends are the series' own min and max, never widened to take
// in the current value: a value outside them puts the dot at the nearer end
// and the accessible name says it is above the window max or below its min.
// Without a series there is no track; "steady at N all window" needs a
// series whose min equals its max. A series that begins more than a bucket
// and a half after the window's start says "since HH:MM UTC" in place of
// the window, as the history is younger than the range.
import { RLink } from "./common";
import { useId, useState, type ReactNode } from "react";
import type { Level, Series, Window } from "../api/model";
import { rangeText } from "../charts/chartLabel";
import { Sparkline } from "../charts/Sparkline";
import { ChartHistoryStatus } from "../charts/HistoryGaps";
import { dayMonHm, hm } from "../lib/format";
import { isTrouble } from "../lib/view";

const DAY = 86_400_000;

/**
 * When the series begins, when that is more than a bucket and a half
 * after the window's start; undefined when it covers the window.
 */
export function seriesSince(series: Series | undefined, start: number): number | undefined {
  const first = series?.points[0]?.time;
  if (first === undefined || !series?.stepMs) return undefined;
  return first - start > 1.5 * series.stepMs ? first : undefined;
}

/** "since 14:30 UTC", with the date when it is a day or more before the end. */
export function sinceText(since: number, end: number): string {
  return `since ${end - since < DAY ? hm(since) : dayMonHm(since)} UTC`;
}

export interface MetricBoxProps {
  title: string;
  /** Optional destination; leaves the chart and info button independently interactive. */
  to?: string;
  /** Describes the headline measurement separately from the history below. */
  valueLabel?: string;
  /** What the metric measures, shown behind the (i) button. */
  description: string;
  series?: Series;
  /** A second series drawn dashed, for example messages out beside in. */
  series2?: Series;
  /** The current value; the newest point when not given; null when there is none now. */
  value?: number | null;
  /** Replaces the formatted value at the top right, for example "61 / 63 /s". */
  valueText?: string;
  fmt: (v: number) => string;
  status?: Level;
  /** The word shown when the status is not ok; the status's name by default. */
  stateWord?: string;
  note?: ReactNode;
  start: number;
  end: number;
  windows?: Window[];
  loading?: boolean;
  /** Why the series could not be loaded. */
  error?: string;
  /**
   * Whether to draw the range track; true by default. Off for a box whose
   * value is a name, such as the meta leader, which no min and max describe.
   */
  track?: boolean;
  /** Historical sample formatting, independently of a text headline. */
  bucketNote?: string;
  sampleFmt?: (value: number) => string;
  seriesLabels?: [string, string];
  /** Whether to draw the chart; true by default. A box without one is compact. */
  chart?: boolean;
  /** The chart's colouring, as `Sparkline` takes it. */
  tone?: "status" | "change";
}

/** An (i) button that shows a description on hover, focus or click. */
export function InfoTip({ label, text }: { label: string; text: string }) {
  // Hover shows it while the pointer stays; a click pins it until the next
  // click, blur or Escape.
  const [hover, setHover] = useState(false);
  const [pinned, setPinned] = useState(false);
  const open = hover || pinned;
  const id = useId();
  const close = () => {
    setHover(false);
    setPinned(false);
  };
  return (
    <span className="infotip" onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}>
      <button
        type="button"
        className="infotip-b"
        aria-label={`About ${label}`}
        aria-expanded={open}
        aria-describedby={open ? id : undefined}
        onClick={() => setPinned((o) => !o)}
        onFocus={() => setHover(true)}
        onBlur={close}
        onKeyDown={(e) => e.key === "Escape" && close()}
      >
        i
      </button>
      {open && (
        <span role="tooltip" id={id} className="infotip-t">
          {text}
        </span>
      )}
    </span>
  );
}

/** The dot's tint: info too, quietly. */
function stateClass(status: Level): string {
  return status === "critical" || status === "warning" || status === "info" ? ` st-${status}` : "";
}

/** The box's class: only trouble colours the value and shows a word. */
function alertClass(status: Level): string {
  return isTrouble(status) ? ` st-${status}` : "";
}

/**
 * The range track: the window's min left, its max right, a dot at the value
 * clamped to the ends. Nothing without both edges; "steady" when they are
 * equal; no dot without a current value.
 */
export function RangeTrack({
  title,
  min,
  max,
  value,
  fmt,
  status,
  period,
  since,
  noCurrent = false,
  history,
}: {
  title: string;
  min?: number;
  max?: number;
  value?: number;
  fmt: (v: number) => string;
  status: Level;
  period: string;
  /** "since 14:30 UTC" when the series begins after the window does. */
  since?: string;
  noCurrent?: boolean;
  /** The chart's domain, when the box draws one: its history status then shows in this row. */
  history?: { start: number; end: number };
}) {
  const inRow = (text: ReactNode) => (history ? <ChartHistoryStatus start={history.start} end={history.end}>{text}</ChartHistoryStatus> : text);
  if (min === undefined || max === undefined) return null;
  if (min === max) {
    // Without a current value a steady window says so, so a server that
    // stopped never reads as quietly steady.
    const over = since ?? (noCurrent ? "in the window" : "all window");
    return <div className="mb-track-note">{inRow(noCurrent ? `steady at ${fmt(min)} ${over} · no current value` : `steady at ${fmt(min)} ${over}`)}</div>;
  }
  const frac = value === undefined ? undefined : Math.min(1, Math.max(0, (value - min) / (max - min)));
  const where = value === undefined ? "no current value" : `current ${fmt(value)}${value > max ? ", above the window max" : value < min ? ", below the window min" : ""}`;
  return (
    <div className="mb-range" role="img" aria-label={`${title}, ${where}, min ${fmt(min)}, max ${fmt(max)}, ${since ?? period}`}>
      <div className="mb-track">{frac !== undefined && <span className={`mb-dot${stateClass(status)}`} style={{ left: `${+(frac * 100).toFixed(1)}%` }} />}</div>
      <div className="mb-ends" aria-hidden="true">
        <span>{fmt(min)}</span>
        {inRow(since && <span className="mb-since">{since}</span>)}
        <span>{fmt(max)}</span>
      </div>
    </div>
  );
}

export function MetricBox(p: MetricBoxProps) {
  const status = p.status ?? "ok";
  const track = p.track ?? true;
  const pts = p.series?.points ?? [];
  const value = p.value === null ? undefined : (p.value ?? (pts.length ? pts[pts.length - 1].value : undefined));
  const hasWindow = pts.length > 0 && p.series?.min !== undefined && p.series?.max !== undefined;
  const min = hasWindow ? p.series!.min : undefined;
  const max = hasWindow ? p.series!.max : undefined;
  const shown = p.loading ? "…" : (p.valueText ?? (value === undefined ? "–" : p.fmt(value)));
  const alert = alertClass(status);
  const word = alert ? (p.stateWord ?? status) : undefined;
  const period = rangeText(p.start, p.end);
  const first = seriesSince(p.series, p.start);
  const since = first === undefined ? undefined : sinceText(first, p.end);
  const chart = p.chart ?? true;
  // Whether a chart is drawn, the only case with a history read of its own.
  const drawn = chart && !(p.error && !p.series);
  return (
    <div className={`card mbox${alert}${p.to ? " chart-linked" : ""}${chart ? "" : " mb-quiet"}`}>
      <div className="mb-h">
        <span className="lbl">
          {p.to ? <RLink to={p.to} className="mb-t chart-link">{p.title}</RLink> : <span className="mb-t">{p.title}</span>}
          <InfoTip label={p.title} text={p.description} />
        </span>
        <span className="mb-v">
          {word && <em className="mb-word">{word}</em>}
          <b className="mono">{shown}</b>
        </span>
      </div>
      {p.valueLabel && <div className="mb-context">{p.valueLabel}</div>}
      {p.error && p.series && (
        <span className="mb-stale" title={p.error}>
          refresh failed
        </span>
      )}
      {p.error && !p.series ? (
        <p className="muted-note">Could not load: {p.error}</p>
      ) : chart && (
        <Sparkline
          points={pts}
          stepMs={p.series?.stepMs}
          bucketNote={p.bucketNote ?? (p.series?.unit === "count" ? "Last count in bucket" : "Mean value in bucket")}
          fmt={p.sampleFmt ?? p.fmt}
          labels={p.seriesLabels}
          points2={p.series2?.points}
          status={status}
          windows={p.windows ?? []}
          start={p.start}
          end={p.end}
          label={p.title}
          current={p.valueText ?? (value === undefined ? undefined : p.fmt(value))}
          tone={p.tone}
        />
      )}
      {track && <RangeTrack title={p.title} min={min} max={max} value={value} fmt={p.fmt} status={status} period={period} since={since} noCurrent={p.value === null} history={drawn ? { start: p.start, end: p.end } : undefined} />}
      {track && !hasWindow && !p.error && !p.loading && <div className="mb-track-note">{drawn ? <ChartHistoryStatus start={p.start} end={p.end}>no data for the window</ChartHistoryStatus> : "no data for the window"}</div>}
      {track && hasWindow && p.value === null && min !== max && <div className="mb-track-note">no current value</div>}
      {p.note && <div className="mb-note">{p.note}</div>}
    </div>
  );
}

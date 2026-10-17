// A metric box: a title with an (i) description, the current value top
// right, a line chart, and a range track with the window's min on the left,
// its max on the right and a dot at the current value. Health colours the
// value and the dot, with a word only when it is not ok. A configured limit
// belongs in the note, not on the track.
import { useId, useState, type ReactNode } from "react";
import type { Level, Series, Window } from "../api/model";
import { rangeText } from "../charts/chartLabel";
import { Sparkline } from "../charts/Sparkline";

export interface MetricBoxProps {
  title: string;
  /** What the metric measures, shown behind the (i) button. */
  description: string;
  series?: Series;
  /** A second series drawn dashed, for example messages out beside in. */
  series2?: Series;
  /** The current value; the newest point when not given. */
  value?: number;
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

function stateClass(status: Level): string {
  return status === "critical" || status === "warning" || status === "info" ? ` st-${status}` : "";
}

/** The range track: min left, max right, a dot at the value; steady or no data in words. */
export function RangeTrack({ title, min, max, value, fmt, status, period }: { title: string; min?: number; max?: number; value?: number; fmt: (v: number) => string; status: Level; period: string }) {
  if (value === undefined || min === undefined || max === undefined) {
    return <div className="mb-track-note">no data</div>;
  }
  if (min === max) {
    return <div className="mb-track-note">steady at {fmt(min)} all window</div>;
  }
  const frac = Math.min(1, Math.max(0, (value - min) / (max - min)));
  return (
    <div className="mb-range" role="img" aria-label={`${title}, current ${fmt(value)}, min ${fmt(min)}, max ${fmt(max)}, ${period}`}>
      <div className="mb-track">
        <span className={`mb-dot${stateClass(status)}`} style={{ left: `${+(frac * 100).toFixed(1)}%` }} />
      </div>
      <div className="mb-ends" aria-hidden="true">
        <span>{fmt(min)}</span>
        <span>{fmt(max)}</span>
      </div>
    </div>
  );
}

export function MetricBox(p: MetricBoxProps) {
  const status = p.status ?? "ok";
  const pts = p.series?.points ?? [];
  const value = p.value ?? (pts.length ? pts[pts.length - 1].value : undefined);
  // The window's min and max always take in the current value, which may
  // come from outside the series.
  const min = p.series?.min === undefined ? value : value === undefined ? p.series.min : Math.min(p.series.min, value);
  const max = p.series?.max === undefined ? value : value === undefined ? p.series.max : Math.max(p.series.max, value);
  const shown = p.loading ? "…" : (p.valueText ?? (value === undefined ? "–" : p.fmt(value)));
  const alert = stateClass(status);
  const word = alert ? (p.stateWord ?? status) : undefined;
  const period = rangeText(p.start, p.end);
  return (
    <div className={`card mbox${alert}`}>
      <div className="mb-h">
        <span className="lbl">
          {p.title}
          <InfoTip label={p.title} text={p.description} />
        </span>
        <span className="mb-v">
          {word && <em className="mb-word">{word}</em>}
          <b className="mono">{shown}</b>
        </span>
      </div>
      {p.error && !p.series ? (
        <p className="muted-note">Could not load: {p.error}</p>
      ) : (
        <Sparkline
          points={pts}
          points2={p.series2?.points}
          status={status}
          windows={p.windows ?? []}
          start={p.start}
          end={p.end}
          label={p.title}
          current={value === undefined ? undefined : p.fmt(value)}
        />
      )}
      <RangeTrack title={p.title} min={min} max={max} value={value} fmt={p.fmt} status={status} period={period} />
      {p.note && <div className="mb-note">{p.note}</div>}
    </div>
  );
}

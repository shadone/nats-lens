// The tile's min-to-max range bar, and the storage variant filled up to a
// limit.

/** `label` names the metric and `period` the time the min and max cover. */
export function RangeBar({ min, max, current, fmt, label, period }: { min: number; max: number; current: number; fmt: (v: number) => string; label: string; period: string }) {
  const frac = max === min ? 1 : Math.min(1, Math.max(0, (current - min) / (max - min)));
  return (
    <div className="rng" role="img" aria-label={`${label}, current ${fmt(current)}, min ${fmt(min)}, max ${fmt(max)}, ${period}`}>
      <div className="track">
        <div className="fill" style={{ left: 0, right: 0 }} />
        <div className="cur" style={{ left: `${(frac * 100).toFixed(1)}%` }} />
      </div>
      <div className="ends" aria-hidden="true">
        <span>min {fmt(min)}</span>
        <span>max {fmt(max)}</span>
      </div>
    </div>
  );
}

export function LimitBar({ max, current, limit, limitText, label, currentText }: { max: number; current: number; limit: number; limitText: string; label: string; currentText: string }) {
  const used = limit > 0 ? (current / limit) * 100 : 0;
  const fill = limit > 0 ? Math.min(100, (max / limit) * 100) : 0;
  return (
    <div className="rng" role="img" aria-label={`${label}, current ${currentText}, ${used.toFixed(1)}% of ${limitText} used`}>
      <div className="track">
        <div className="fill acc" style={{ left: 0, width: `${fill.toFixed(2)}%` }} />
      </div>
      <div className="ends" aria-hidden="true">
        <span>0</span>
        <span>
          {used.toFixed(1)}% of {limitText}
        </span>
      </div>
    </div>
  );
}

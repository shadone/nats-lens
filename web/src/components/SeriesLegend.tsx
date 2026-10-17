// A chart legend that draws the mark it explains: the band's fill, a
// line's stroke, the dashed line.

export type Mark = "band" | "median" | "busiest" | "line" | "dashed";

export interface LegendItem {
  mark: Mark;
  label: string;
}

/** The mark alone, 22 by 10 pixels, with the same class the chart uses. */
export function MarkSwatch({ mark }: { mark: Mark }) {
  return (
    <svg width="22" height="10" viewBox="0 0 22 10" aria-hidden="true" className="mk">
      {mark === "band" ? <rect x="0" y="1" width="22" height="8" className="sc-band" /> : <line x1="0" y1="5" x2="22" y2="5" className={`sc-${mark}`} />}
    </svg>
  );
}

export function SeriesLegend({ items }: { items: LegendItem[] }) {
  return (
    <ul className="sc-legend">
      {items.map((i) => (
        <li key={i.mark + i.label}>
          <MarkSwatch mark={i.mark} />
          {i.label}
        </li>
      ))}
    </ul>
  );
}

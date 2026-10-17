// A chart legend that draws each series' own mark: its stroke colour, its
// dash, its fill. The classes are the ones the chart draws with, so the
// legend cannot drift from the chart.

/** A colour role of a mark; each has a light and a dark value. */
export type Tone = "accent" | "muted" | "info" | "warn";

/** How a series is drawn: a line, a filled band between two edges, or bars. */
export type MarkKind = "line" | "band" | "bars";

export interface MarkStyle {
  tone: Tone;
  /** A dashed stroke. */
  dash?: boolean;
  /** For a band: draw its upper edge as a line too. */
  edge?: boolean;
}

export interface LegendItem {
  label: string;
  kind: MarkKind;
  style: MarkStyle;
}

/** Classes of a line stroke in this style. */
export function strokeClass(s: MarkStyle): string {
  return `tc-line tk-${s.tone}${s.dash ? " tc-dash" : ""}`;
}

/** Classes of a band's or a bar's fill in this style. */
export function fillClass(kind: "band" | "bars", s: MarkStyle): string {
  return `${kind === "bars" ? "tc-bar" : "tc-fill"} tk-${s.tone}`;
}

/** The mark alone, 22 by 10 pixels. */
export function Swatch({ kind, style }: { kind: MarkKind; style: MarkStyle }) {
  return (
    <svg className="lg-sw" width="22" height="10" viewBox="0 0 22 10" aria-hidden="true">
      {kind === "line" && <line x1="1" y1="5" x2="21" y2="5" className={strokeClass(style)} />}
      {kind === "band" && (
        <>
          <rect x="0" y="1" width="22" height="9" className={fillClass("band", style)} />
          {style.edge && <line x1="0" y1="1" x2="22" y2="1" className={strokeClass(style)} />}
        </>
      )}
      {kind === "bars" && (
        <>
          <rect x="5" y="2" width="3" height="8" className={fillClass("bars", style)} />
          <rect x="14" y="5" width="3" height="5" className={fillClass("bars", style)} />
        </>
      )}
    </svg>
  );
}

export function Legend({ items, label = "Legend" }: { items: LegendItem[]; label?: string }) {
  if (items.length === 0) return null;
  return (
    <ul className="lg" aria-label={label}>
      {items.map((i) => (
        <li key={i.kind + i.label}>
          <Swatch kind={i.kind} style={i.style} />
          {i.label}
        </li>
      ))}
    </ul>
  );
}

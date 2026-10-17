// The Overview's nine tiles as metric boxes: the API's description behind
// (i), the value top right, the chart and the window's range track. A
// tile's limit goes in the note, never on the track. A value the API did
// not observe is a dash with no dot.
import type { Tile, Window } from "../api/model";
import { tileValue, fmtValue } from "../lib/units";
import { MetricBox } from "./MetricBox";

function join(...parts: string[]): string {
  return parts.filter(Boolean).join(" ");
}

/** The text top right: "3 / 3", "768 MiB", "61 / 63 /s"; undefined without a value. */
export function tileText(t: Tile): string | undefined {
  if (t.valueText) return t.valueText;
  if (t.value === undefined) return undefined;
  const [v, u] = tileValue(t.value, t.unit);
  // A limit is said in the note, so its "of 1.8 GiB" suffix is left out here.
  const main = t.limit !== undefined ? join(v, u) : join(v, u, t.valueSuffix);
  if (t.value2 === undefined) return main;
  const [v2, u2] = tileValue(t.value2, t.unit);
  return `${main}, ${join(v2, u2, t.value2Suffix)}`;
}

export function tileNote(t: Tile): string {
  const limit = t.limit !== undefined ? `limit ${fmtValue(t.limit, t.unit)}` : "";
  return [t.subtitle, limit].filter(Boolean).join(" · ");
}

export function OverviewTiles({ tiles, windows, start, end }: { tiles: Tile[]; windows: Window[]; start: number; end: number }) {
  return (
    <div className="ov-tiles">
      {tiles.map((t) => (
        <MetricBox
          key={t.id}
          title={t.label}
          description={t.description ?? ""}
          series={t.series}
          series2={t.series2}
          // A name tile (the meta leader) has its count of changes as value;
          // its track follows the newest point of the changes series.
          value={t.valueText ? undefined : (t.value ?? null)}
          valueText={tileText(t)}
          fmt={(v) => fmtValue(v, t.unit)}
          status={t.status}
          note={tileNote(t) || undefined}
          start={start}
          end={end}
          windows={windows}
        />
      ))}
    </div>
  );
}

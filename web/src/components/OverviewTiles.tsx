import { HistoryLink, usePageHistory } from "./HistoryNote";
import { rangeCoverageText } from "../lib/historyView";
// The Overview's nine tiles as metric boxes: the API's description behind
// (i), the value top right, the chart and the window's range track. A
// tile's limit goes in the note, never on the track. A value the API did
// not observe is a dash with no dot.
import type { Level, Tile, Window } from "../api/model";
import { tileValue, fmtValue } from "../lib/units";
import { MetricBox } from "./MetricBox";

const destinations: Record<string, string> = { servers: "/servers", meta_leader: "/servers", streams: "/streams", kv: "/kv-stores", object_stores: "/object-stores", consumers: "/consumers", accounts: "/accounts", clients: "/connections", storage: "/capacity" };

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

/**
 * The tile's status. No data with a value means no check judges the
 * number, as for Accounts: the tile then reads as ok, never as missing.
 */
export function tileStatus(t: Tile): Level {
  return t.status === "nodata" && (t.value !== undefined || !!t.valueText) ? "ok" : t.status;
}

export function OverviewTiles({ tiles, windows, start, end }: { tiles: Tile[]; windows: Window[]; start: number; end: number }) {
  const h = usePageHistory();
  const coverage = rangeCoverageText(h?.historyStart, start, end);
  // Tile growth is conveyed in its subtitle, without typed readiness facts.
  // Keep this guidance neutral rather than infer missing data from retained age.
  const growth = h ? "Growth uses samples spanning 6 h; entity measurements determine the rate." : undefined;
  return (
    <div className="ov-tiles">
      {tiles.map((t) => (
        <MetricBox
          key={t.id}
          title={t.label}
          to={destinations[t.id]}
          valueLabel={t.id === "meta_leader" ? "Newest leader · chart: changes per bucket" : t.id === "storage" ? "Newest storage reports" : "Latest count bucket"}
          description={t.description ?? ""}
          series={t.series}
          series2={t.series2}
          value={t.valueText ? undefined : (t.value ?? null)}
          valueText={tileText(t)}
          // A name has no min and max: the chart shows its changes, without a track.
          track={!t.valueText}
          bucketNote={t.id === "meta_leader" ? "Total leader changes in bucket; leader name is from the newest collection" : undefined}
          sampleFmt={t.id === "meta_leader" ? v => `${v} leader ${v === 1 ? "change" : "changes"}` : undefined}
          seriesLabels={t.id === "meta_leader" ? ["changes", "changes"] : undefined}
          fmt={(v) => fmtValue(v, t.unit)}
          status={tileStatus(t)}
          note={<>{tileNote(t)}{coverage && ["clients", "storage"].includes(t.id) && ` · ${coverage}`}{t.id === "storage" && growth && <> · <HistoryLink>{growth}</HistoryLink></>}</>}
          start={start}
          end={end}
          windows={windows}
        />
      ))}
    </div>
  );
}

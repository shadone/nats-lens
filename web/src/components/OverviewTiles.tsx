// The Overview's nine tiles as metric boxes: the API's description behind
// (i), the value top right, the chart and the window's range track. A
// tile's limit goes in the note, never on the track. A value the API did
// not observe is a dash with no dot. A tile draws its chart only when
// `tileChartReason` does not call it steady.
import { HistoryLink, usePageHistory } from "./HistoryNote";
import type { HistoryGap } from "../api/history";
import type { Level, Tile, Window } from "../api/model";
import { useDataHistory } from "../api/queries";
import { rangeCoverageText } from "../lib/historyView";
import { tileChartReason } from "../lib/tileChart";
import { useTimeRange } from "../lib/useTimeRange";
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

interface TilesProps {
  tiles: Tile[];
  windows: Window[];
  start: number;
  end: number;
}

/**
 * The tiles with the range's collection gaps. Without a page history the
 * charts hatch no gaps either, so none are assumed.
 */
export function OverviewTiles(p: TilesProps) {
  const h = usePageHistory();
  return h ? <TilesWithGaps {...p} /> : <TileGrid {...p} gaps={[]} />;
}

function TilesWithGaps(p: TilesProps) {
  const { spec } = useTimeRange();
  const d = useDataHistory(spec).data;
  // The newest gap the answer left out of its list counts as well.
  const gaps = d && (d.newestOmittedCollectionGap ? [...d.gaps, d.newestOmittedCollectionGap] : d.gaps);
  return <TileGrid {...p} gaps={gaps} />;
}

function TileGrid({ tiles, windows, start, end, gaps }: TilesProps & { gaps?: readonly HistoryGap[] }) {
  const h = usePageHistory();
  const coverage = rangeCoverageText(h?.historyStart, start, end);
  // Tile growth is conveyed in its subtitle, without typed readiness facts.
  // Keep this guidance neutral rather than infer missing data from retained age.
  const growth = h ? "Growth uses samples spanning 6 h; entity measurements determine the rate." : undefined;
  return (
    <div className="ov-tiles">
      {tiles.map((t) => {
        const leader = t.id === "meta_leader";
        const status = tileStatus(t);
        const chart = tileChartReason({ points: t.series?.points ?? [], points2: t.series2?.points, stepMs: t.series?.stepMs, min: t.series?.min, max: t.series?.max, start, end, status, changesSeries: leader, live: h?.live, historyStart: h?.historyStart, gaps }) !== "steady";
        return <MetricBox
          key={t.id}
          title={t.label}
          to={destinations[t.id]}
          // The leader's chart counts changes, not the name above it.
          valueLabel={leader && chart ? "Leader changes per bucket" : undefined}
          chart={chart}
          tone="change"
          description={t.description ?? ""}
          series={t.series}
          series2={t.series2}
          value={t.id === "meta_leader" ? null : t.valueText ? undefined : (t.value ?? null)}
          valueText={t.id === "meta_leader" ? (t.valueText || "Unknown") : tileText(t)}
          // A name has no min and max: the chart shows its changes, without a track.
          track={t.id !== "meta_leader" && !t.valueText}
          bucketNote={t.id === "meta_leader" ? "Total leader changes in bucket; leader name is from the newest collection" : undefined}
          sampleFmt={t.id === "meta_leader" ? v => `${v} leader ${v === 1 ? "change" : "changes"}` : undefined}
          seriesLabels={t.id === "meta_leader" ? ["changes", "changes"] : undefined}
          fmt={(v) => fmtValue(v, t.unit)}
          status={status}
          note={<>{tileNote(t)}{coverage && ["clients", "storage"].includes(t.id) && ` · ${coverage}`}{t.id === "storage" && growth && <> · <HistoryLink>{growth}</HistoryLink></>}</>}
          start={start}
          end={end}
          windows={windows}
        />;
      })}
    </div>
  );
}

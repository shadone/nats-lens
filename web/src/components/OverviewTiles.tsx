// The Overview's nine tiles as metric boxes: the API's description behind
// (i), the value top right, the chart and the window's range track. A
// tile's limit goes in the note, never on the track. A value the API did
// not observe is a dash with no dot. A tile whose number is read over the
// range says so when the history inside the range is shorter ("over
// 31 min"), and the storage tile how long its growth rate still needs.
import type { ReactNode } from "react";
import type { Level, Tile, Window } from "../api/model";
import { growthWaitText, tileSpanText, type ChartGaps, type HistoryFrame } from "../lib/historyNotes";
import { tileValue, fmtValue } from "../lib/units";
import { HistoryLink } from "./HistoryNote";
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

/**
 * The tile's status. No data with a value means no check judges the
 * number, as for Accounts: the tile then reads as ok, never as missing.
 */
export function tileStatus(t: Tile): Level {
  return t.status === "nodata" && (t.value !== undefined || !!t.valueText) ? "ok" : t.status;
}

/** The tiles whose number is read over the range: slow consumers in it, and the storage growth. */
export const RANGE_TILES = new Set(["clients", "storage"]);

/** The history the tiles see: where it begins, and the collect interval. */
export interface TileHistory extends Omit<HistoryFrame, "start" | "end"> {
  intervalMs: number;
}

/** A tile's note with what the history inside the range adds to it. */
export function tileNoteNode(t: Tile, h: TileHistory | undefined, start: number, end: number): ReactNode {
  const base = tileNote(t);
  if (!h || !RANGE_TILES.has(t.id)) return base || undefined;
  const span = tileSpanText({ historyStart: h.historyStart, start, end }, h.intervalMs);
  // The server writes a growth it has into the subtitle ("+30 MiB/day"); only a tile without one waits.
  const wait = t.id === "storage" && t.value !== undefined && !t.subtitle.includes("/day") ? growthWaitText({ ...h, start, end }) : undefined;
  const text = [base, span].filter(Boolean).join(" · ");
  if (!wait) return text || undefined;
  return (
    <>
      {text && `${text} · `}
      <span className="hn-wait">growth {wait}</span> <HistoryLink />
    </>
  );
}

export function OverviewTiles({
  tiles,
  windows,
  start,
  end,
  gaps,
  history,
}: {
  tiles: Tile[];
  windows: Window[];
  start: number;
  end: number;
  /** The range's times without collections, hatched on every tile. */
  gaps?: ChartGaps;
  history?: TileHistory;
}) {
  return (
    <div className="ov-tiles">
      {tiles.map((t) => (
        <MetricBox
          key={t.id}
          title={t.label}
          description={t.description ?? ""}
          series={t.series}
          series2={t.series2}
          value={t.valueText ? undefined : (t.value ?? null)}
          valueText={tileText(t)}
          // A name has no min and max: the chart shows its changes, without a track.
          track={!t.valueText}
          fmt={(v) => fmtValue(v, t.unit)}
          status={tileStatus(t)}
          note={tileNoteNode(t, history, start, end)}
          start={start}
          end={end}
          windows={windows}
          gaps={gaps}
        />
      ))}
    </div>
  );
}

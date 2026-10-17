// "Cluster now" number tiles: value, sparkline and min-max range.
import type { Tile, Window } from "../api/model";
import { LimitBar, RangeBar } from "../charts/RangeBar";
import { Sparkline } from "../charts/Sparkline";
import { rangeText } from "../charts/chartLabel";
import { fmtBytes, fmtCount } from "../lib/format";
import { fmtValue, tileValue } from "../lib/units";
import { Glyph } from "./Severity";

// The range and limit texts use the unit's own formatter, as the charts do.
function rangeFmt(t: Tile) {
  return (v: number) => fmtValue(v, t.unit);
}

export function TileCard({ t, windows, start, end }: { t: Tile; windows: Window[]; start: number; end: number }) {
  const [v, u] = tileValue(t.value, t.unit);
  const [v2, u2] = tileValue(t.value2, t.unit);
  const dual = t.value2 !== undefined || t.value2Suffix !== "";
  const alert = t.status === "critical" || t.status === "warning" || t.status === "info";
  // The range always takes in the current value, which the API reports
  // apart from the series.
  const min = t.series?.min === undefined ? undefined : t.value === undefined ? t.series.min : Math.min(t.series.min, t.value);
  const max = t.series?.max === undefined ? undefined : t.value === undefined ? t.series.max : Math.max(t.series.max, t.value);
  const join = (...parts: string[]) => parts.filter(Boolean).join(" ");
  const current = t.value === undefined ? undefined : join(v, u, t.valueSuffix) + (t.value2 !== undefined ? `, ${join(v2, u2, t.value2Suffix)}` : "");
  return (
    <div className={`card tile${dual ? " dual" : ""}${alert ? ` st-${t.status}` : ""}`}>
      <span className="lbl">
        {t.label}
        {alert && <Glyph level={t.status} title={t.status} />}
      </span>
      <div className="val">
        <span className="g">
          <b>{v}</b> <em>{[u, t.valueSuffix].filter(Boolean).join(" ")}</em>
        </span>
        {dual && (
          <>
            <span className="sep" />
            <span className="g">
              <b className="b2">{v2}</b> <em>{[u2, t.value2Suffix].filter(Boolean).join(" ")}</em>
            </span>
          </>
        )}
      </div>
      <div className="sub">{t.subtitle}</div>
      <Sparkline points={t.series?.points ?? []} points2={t.series2?.points} status={t.status} windows={windows} start={start} end={end} label={t.label} current={current} />
      {t.limit !== undefined && max !== undefined ? (
        <LimitBar max={max} current={t.value ?? max} limit={t.limit} limitText={t.unit === "bytes" ? fmtBytes(t.limit) : fmtCount(t.limit)} label={t.label} currentText={rangeFmt(t)(t.value ?? max)} />
      ) : min !== undefined && max !== undefined ? (
        <RangeBar min={min} max={max} current={t.value ?? min} fmt={rangeFmt(t)} label={t.label} period={rangeText(start, end)} />
      ) : (
        <div className="rng">
          <div className="ends">
            <span>no history in range</span>
          </div>
        </div>
      )}
    </div>
  );
}

export function Tiles({ tiles, windows, start, end }: { tiles: Tile[]; windows: Window[]; start: number; end: number }) {
  return (
    <div className="tiles">
      {tiles.map((t) => (
        <TileCard key={t.id} t={t} windows={windows} start={start} end={end} />
      ))}
    </div>
  );
}

// The status timeline of the reference: a label column, a cell track that
// scrolls sideways on narrow screens and opens at now, and a now column. A
// Kubernetes lane sits on top when the range has Kubernetes events; a table
// gives the same content as text.
import { useLayoutEffect, useRef, useState, type CSSProperties, type RefObject } from "react";
import type { Level, Timeline, TimelineRow } from "../api/model";
import { hm } from "../lib/format";
import { placeLaneAnnotations } from "../lib/laneLayout";
import { axisTicks, cellRuns, isDeployment, laneAnnotations, runsText } from "../lib/timelineModel";
import { useWidth } from "../lib/useWidth";
import { LEVEL_LABEL } from "../lib/view";
import { Glyph, Sev } from "./Severity";

export const CELL_FILL: Record<Level, string> = {
  ok: "var(--ok-cell)",
  info: "var(--cell-info)",
  warning: "var(--cell-warning)",
  critical: "var(--cell-critical)",
  muted: "var(--track-fill)",
  nodata: "var(--surface-sunk)",
};

const CW = 960;

export function Cells({ row, start, bucketMs }: { row: TimelineRow; start: number; bucketMs: number }) {
  const cw = CW / Math.max(1, row.cells.length);
  const runs = runsText(cellRuns(row.cells, start, bucketMs));
  return (
    <svg className="cells" viewBox={`0 0 ${CW} 16`} preserveAspectRatio="none" role="img" aria-label={`${row.checkCode}: ${runs}`}>
      {row.cells.map((lv, i) => (
        <rect key={i} x={(i * cw + 0.6).toFixed(2)} y="0" width={Math.max(0.5, cw - 1.6).toFixed(2)} height="16" rx="1.5" style={{ fill: CELL_FILL[lv] }} data-level={lv}>
          <title>{`${hm(start + i * bucketMs)} ${LEVEL_LABEL[lv].toLowerCase()}`}</title>
        </rect>
      ))}
    </svg>
  );
}

export function trackSpan(tl: Timeline, end: number): number {
  return tl.bucketCount * tl.bucketMs || end - tl.start || 1;
}

function K8sLane({ tl, scroller, end }: { tl: Timeline; scroller: RefObject<HTMLDivElement | null>; end: number }) {
  const lane = useRef<HTMLDivElement>(null);
  const W = useWidth(lane);
  const span = trackSpan(tl, end);
  const frac = (t: number) => (t - tl.start) / span;
  const visible = scroller.current?.clientWidth ?? W;
  const placed = W ? placeLaneAnnotations(laneAnnotations(tl, (t) => frac(t) * W), Math.max(0, W - visible), W) : [];
  return (
    <div className="lane k8slane" ref={lane}>
      <div className="base" />
      {tl.windows.map((w) => (
        <div key={w.start} className="brk" style={{ left: `${(frac(w.start) * 100).toFixed(3)}%`, width: `${((frac(w.end) - frac(w.start)) * 100).toFixed(3)}%` }} />
      ))}
      {tl.kubernetes.map((e) => (
        <span key={e.id} className={`tick${isDeployment(e) ? " deploy" : ""}`} style={{ left: `${(frac(e.time) * 100).toFixed(3)}%` }} title={`${hm(e.time)} ${e.label}`} />
      ))}
      {placed.map((p) => (
        <span key={p.a} className="ann" style={{ left: `${p.l.toFixed(1)}px`, width: `${(p.r - p.l).toFixed(1)}px`, textAlign: p.align, [p.align === "right" ? "paddingRight" : "paddingLeft"]: 6 }}>
          {p.a}
          <small>{p.b}</small>
        </span>
      ))}
    </div>
  );
}

function TimelineTable({ tl }: { tl: Timeline }) {
  return (
    <div className="table-wrap">
      <table className="tbl">
        <caption className="sr-only">Status per check over the range</caption>
        <thead>
          <tr>
            <th scope="col">Check</th>
            <th scope="col">Name</th>
            <th scope="col">Now</th>
            <th scope="col">Not ok, UTC</th>
          </tr>
        </thead>
        <tbody>
          {tl.rows.map((r) => (
            <tr key={r.key}>
              <td className="mono">{r.checkCode}</td>
              <td>{r.checkName}</td>
              <td>{LEVEL_LABEL[r.now]}</td>
              <td className="mono">{runsText(cellRuns(r.cells, tl.start, tl.bucketMs))}</td>
            </tr>
          ))}
          {tl.kubernetes.map((e) => (
            <tr key={e.id}>
              <td className="mono">k8s</td>
              <td colSpan={2}>{e.label}</td>
              <td className="mono">{hm(e.time)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function StatusTimeline({ tl, end, live, asTable }: { tl: Timeline; end: number; live: boolean; asTable: boolean }) {
  const scroller = useRef<HTMLDivElement>(null);
  const [, redraw] = useState(0);
  const span = trackSpan(tl, end);
  const frac = (t: number) => (t - tl.start) / span;
  const hasLane = tl.kubernetes.length > 0;
  const win = tl.windows[tl.windows.length - 1];

  // Open the scrolled track at now, then redraw the lane for its visible part.
  useLayoutEffect(() => {
    const sc = scroller.current;
    if (sc && sc.scrollWidth > sc.clientWidth) sc.scrollLeft = sc.scrollWidth;
    redraw((n) => n + 1);
  }, [tl.bucketCount, asTable]);

  if (asTable) return <TimelineTable tl={tl} />;
  const style = { "--buckets": tl.bucketCount } as CSSProperties;
  return (
    <div className="tlx" style={style}>
      <div className="tl-col tl-lab">
        {hasLane && (
          <>
            <div className="lane">
              <span className="code">k8s</span>
              <span className="nm">
                Kubernetes
                <small className="wide">GKE events, {tl.kubernetes.length} in range</small>
                <small className="narrow">{win ? `${win.label.split(" ").pop()} ${hm(win.start)}–${hm(win.end)}` : `${tl.kubernetes.length} events`}</small>
              </span>
            </div>
            <div className="gap" />
          </>
        )}
        {tl.rows.map((r) => (
          <div className="lab" key={r.key}>
            <span className="code">{r.checkCode}</span>
            <span className="nm" title={r.checkName}>
              {r.checkName}
            </span>
          </div>
        ))}
        <div className="axh" />
      </div>
      <div className="tl-sc" ref={scroller} tabIndex={0} aria-label="Status cells, scroll sideways for earlier times">
        <div className="tl-col tl-tr">
          {hasLane && (
            <>
              <K8sLane tl={tl} scroller={scroller} end={end} />
              <div className="gap" />
            </>
          )}
          {tl.rows.map((r) => (
            <div className="crow" key={r.key}>
              <Cells row={r} start={tl.start} bucketMs={tl.bucketMs} />
            </div>
          ))}
          <div className="axh axis" aria-hidden="true">
            {axisTicks(tl.start, tl.start + span, live).map((t) => (
              <span key={`${t.frac}`} className={t.edge ? "edge-r" : undefined} style={{ left: `${(t.frac * 100).toFixed(2)}%` }}>
                {t.label}
              </span>
            ))}
          </div>
          {tl.windows.map((w) => (
            <div key={w.start} className="win" style={{ left: `${(frac(w.start) * 100).toFixed(3)}%`, width: `${((frac(w.end) - frac(w.start)) * 100).toFixed(3)}%` }} />
          ))}
        </div>
      </div>
      <div className="tl-col tl-now">
        {hasLane && (
          <>
            <div className="lane" />
            <div className="gap" />
          </>
        )}
        {tl.rows.map((r) => (
          <div className="now" key={r.key}>
            {r.now === "ok" ? <span className="ok">ok now</span> : <Sev level={r.now} />}
            <Glyph level={r.now === "ok" ? "nodata" : r.now} title={`${LEVEL_LABEL[r.now].toLowerCase()} now`} />
          </div>
        ))}
        <div className="axh" />
      </div>
    </div>
  );
}

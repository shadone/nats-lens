import {Link} from "react-router";
import {useSystem} from "../lib/useSystem";
import {findingHistoryPath} from "../lib/findingHistory";
import {timelineCaptureLimits} from "./TimelineCaptureNotice";
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
import {TimelineFindingRange} from "./TimelineInvestigation";
import {TimelineMark, CheckDetails, WindowPreview, EventPreview, EventDetails, timelineTime} from "./TimelineMark";
import {TimelineEventTicks} from "./TimelineEventTicks";
import {TimelineWindowEvidence} from "./TimelineEventEvidence";
import {evidenceRange,evidenceQuery} from "../lib/timelineEvidence";
import { Glyph, Sev } from "./Severity";

export const CELL_FILL: Record<Level, string> = {
  ok: "var(--ok-cell)",
  info: "var(--cell-info)",
  warning: "var(--cell-warning)",
  critical: "var(--cell-critical)",
  muted: "var(--track-fill)",
  nodata: "var(--surface-sunk)",
};

/**
 * A cell's style. A no-data cell is also outlined in a dashed muted
 * stroke, so it reaches 3:1 against the card in both themes. An ok cell
 * stays faint on purpose: it is the background state, and the cells that
 * are not ok must stand out against it; its level is in the cell's title
 * and the row's accessible name.
 */
export function cellStyle(lv: Level) {
  return lv === "nodata" ? { fill: CELL_FILL[lv], stroke: "var(--text-muted)", strokeWidth: 1, strokeDasharray: "2 1.5" } : { fill: CELL_FILL[lv] };
}

const CW = 960;

export function Cells({ row, start, bucketMs,readEpoch }: { row: TimelineRow; start: number; bucketMs: number;readEpoch?:string }) {
  const cw = CW / Math.max(1, row.cells.length);
  const runs = runsText(cellRuns(row.cells, start, bucketMs));
  return (
    <svg className="cells" viewBox={`0 0 ${CW} 16`} preserveAspectRatio="none" role="group" aria-label={`${row.checkCode}: ${runs}`}>
      {row.cells.map((lv, i) => (
        <TimelineMark svg key={i} details={<><CheckDetails code={row.checkCode}/><TimelineFindingRange readEpoch={readEpoch} from={start+i*bucketMs} end={start+(i+1)*bucketMs} checkCode={row.checkCode} entity={row.entity}/></>} label={`${row.checkCode}: ${LEVEL_LABEL[lv]}`} preview={<><strong>{row.checkCode}: {row.checkName}</strong><div>{timelineTime(start+i*bucketMs)} – {timelineTime(start+(i+1)*bucketMs)}</div><div>Historical bucket: {LEVEL_LABEL[lv]}</div><div>Current check state: {LEVEL_LABEL[row.now]}</div><div>Bucket boundaries, not exact onset or resolution.</div></>}><rect x={(i * cw + 0.6).toFixed(2)} y="0" width={Math.max(0.5, cw - 1.6).toFixed(2)} height="16" rx="1.5" style={cellStyle(lv)} vectorEffect="non-scaling-stroke" data-level={lv}>
        </rect></TimelineMark>
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
        <TimelineMark key={w.start} className="brk" style={{left:`${frac(w.start)*100}%`,width:`${(frac(w.end)-frac(w.start))*100}%`}} label={`Window: ${w.label}`} preview={<WindowPreview {...w}/>} details={<TimelineWindowEvidence window={w} range={tl.evidenceRange??evidenceRange(tl.start,tl.start+span)} readId={tl.eventReadId}/>}/>
      ))}
      <TimelineEventTicks captureLimits={timelineCaptureLimits(tl,"kubernetes")} events={tl.kubernetes} from={tl.start} end={tl.start+span} width={W} range={tl.evidenceRange??evidenceRange(tl.start,tl.start+span)} readId={tl.eventReadId} className={e=>`tick${isDeployment(e)?' deploy':''}`}/>
      {placed.map((p) => (
        <span key={p.a} className="ann" style={{ left: `${p.l.toFixed(1)}px`, width: `${(p.r - p.l).toFixed(1)}px`, textAlign: p.align, [p.align === "right" ? "paddingRight" : "paddingLeft"]: 6 }}>
          {p.a}
          <small>{p.b}</small>
        </span>
      ))}
    </div>
  );
}

function TimelineTable({ tl,history }: { tl: Timeline;history:(row:TimelineRow)=>string }) {
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
              <td className="mono"><Link className="x" aria-label={`${r.checkCode} occurrence history`} to={history(r)}>{r.checkCode}</Link></td>
              <td>{r.checkName}</td>
              <td>{LEVEL_LABEL[r.now]}</td>
              <td className="mono">{runsText(cellRuns(r.cells, tl.start, tl.bucketMs),timelineTime)}</td>
            </tr>
          ))}
          {tl.kubernetes.map((e) => (
            <tr key={e.id}>
              <td className="mono">K8s</td>
              <td colSpan={2}><TimelineMark className="timeline-table-event x" label={`Kubernetes event: ${e.label}`} captureLimits={timelineCaptureLimits(tl,"kubernetes")} preview={<EventPreview event={e}/>} details={<EventDetails event={e} range={tl.evidenceRange??evidenceRange(tl.start,tl.start+tl.bucketCount*tl.bucketMs)} readId={tl.eventReadId} captureLimits={timelineCaptureLimits(tl,"kubernetes")}/>}>{e.label}</TimelineMark></td>
              <td className="mono">{timelineTime(e.time)}</td>
            </tr>
          ))}
          {tl.windows.map((w,i)=><tr key={w.id??`${w.start}:${i}`}><td>Window</td><td colSpan={2}><TimelineMark className="timeline-table-event x" label={`Window: ${w.label}`} preview={<WindowPreview {...w}/>} details={<TimelineWindowEvidence window={w} range={tl.evidenceRange??evidenceRange(tl.start,tl.start+tl.bucketCount*tl.bucketMs)} readId={tl.eventReadId}/>}>{w.label}</TimelineMark></td><td className="mono">{timelineTime(w.start)} – {timelineTime(w.end)}</td></tr>)}
        </tbody>
      </table>
    </div>
  );
}

export function StatusTimeline({ tl, end, live, asTable }: { tl: Timeline; end: number; live: boolean; asTable: boolean }) {
  const system=useSystem();
  const scroller = useRef<HTMLDivElement>(null);
  const [, redraw] = useState(0);
  const span = trackSpan(tl, end);
  const frac = (t: number) => (t - tl.start) / span;
  const fixed=tl.evidenceRange??evidenceRange(tl.start,tl.start+span);
  const history=(row:TimelineRow)=>findingHistoryPath(tl.start,tl.start+span,tl.findingReadEpoch,system,row.checkCode,row.entity,fixed);
  const events=evidenceQuery(system,fixed);events.set("src","kubernetes");events.set("show","all");if(tl.eventReadId!==undefined)events.set("eventReadId",tl.eventReadId);
  const hasLane = tl.kubernetes.length > 0;
  const win = tl.windows[tl.windows.length - 1];

  // Open the scrolled track at now, then redraw the lane for its visible part.
  useLayoutEffect(() => {
    const sc = scroller.current;
    if (sc && sc.scrollWidth > sc.clientWidth) sc.scrollLeft = sc.scrollWidth;
    redraw((n) => n + 1);
  }, [tl.bucketCount, asTable]);

  if (asTable) return <TimelineTable tl={tl} history={history} />;
  const style = { "--buckets": tl.bucketCount } as CSSProperties;
  return (
    <div className="tlx" style={style}>
      <div className="tl-col tl-lab">
        {hasLane && (
          <>
            <div className="lane">
              <span className="code">K8s</span>
              <span className="nm">
                <Link className="x" aria-label="Kubernetes event history" to={`/events?${events}`}>Kubernetes</Link>
                <small className="wide">GKE events, {tl.kubernetes.length} in range</small>
                <small className="narrow">{win ? `${win.label.split(" ").pop()} ${hm(win.start)}–${hm(win.end)}` : `${tl.kubernetes.length} events`}</small>
              </span>
            </div>
            <div className="gap" />
          </>
        )}
        {tl.rows.map((r) => (
          <div className="lab" key={r.key}>
            <Link className="code x" aria-label={`${r.checkCode} occurrence history`} to={history(r)}>{r.checkCode}</Link>
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
              <Cells row={r} start={tl.start} bucketMs={tl.bucketMs} readEpoch={tl.findingReadEpoch} />
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

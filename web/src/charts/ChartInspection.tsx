import {useChartIntervalInteraction} from "./ChartIntervals";
export {ChartIntervalProvider} from "./ChartIntervals";
import {createPortal} from "react-dom";
import {ChartReadout} from "./ChartReadout";
import { InvestigationButton, useInvestigationCopy } from "../components/InvestigationContext";
import type { InvestigationSelection } from "../lib/investigationContext";
// One cursor pattern for compact and expanded charts. Values belong to
// recorded buckets; the cursor never interpolates or carries a value across a gap.
import { useId, useRef, useState, type ReactNode } from "react";
import { dayMonHm, hms } from "../lib/format";
import { usePageHistory } from "../components/HistoryNote";
import { isHatched } from "../api/history";

export interface InspectionSeries {
  label: string;
  points: { time: number; text: string }[];
}

export function inspectionStep(series: InspectionSeries[], given?: number): number {
  if (given && given > 0) return given;
  const times = [...new Set(series.flatMap(s => s.points.map(p => p.time)))].sort((a, b) => a - b);
  const diffs = times.slice(1).map((t, i) => t - times[i]).filter(d => d > 0);
  return diffs.length ? Math.min(...diffs) : 1;
}

export function ChartInspection({ label, start, end, stepMs, bucketNote, series, children, investigation, actionTarget, intervalActions }: {
  label: string; start: number; end: number; stepMs?: number; bucketNote?: string; series: InspectionSeries[]; children: ReactNode; investigation?: InvestigationSelection; actionTarget?: HTMLElement|null;intervalActions?:boolean;
}) {
  const anchor = useRef<HTMLDivElement>(null);
  const lastSelection = useRef<number | undefined>(undefined);
  const interval=useChartIntervalInteraction({label,start,end,source:investigation,enabled:intervalActions});
  const selection = () => {
    if(interval?.highlight)return {...investigation,...interval.highlight,point:undefined,label};
    const point = selected ?? lastSelection.current;
    return { ...investigation, start, end, point: point !== undefined && point >= start && point < end ? point : undefined, label };
  };
  const [cursor, setCursor] = useState<number>();
  const id = useId();
  const history = usePageHistory();
  const step = inspectionStep(series, stepMs);
  const selected = cursor === undefined ? undefined : Math.min(end, Math.max(start, cursor));
  if (selected !== undefined) lastSelection.current = selected;
  const copy = useInvestigationCopy(selection());
  const origin = stepMs ? 0 : (series.flatMap(s => s.points.map(p => p.time)).sort((a,b) => a-b)[0] ?? start);
  const bucket = selected === undefined ? undefined : Math.floor((selected - origin) / step) * step + origin;
  const gaps = history?.gaps?.filter(g => isHatched(g) && selected !== undefined && selected >= (g.start ?? start) && selected < (g.end ?? history.end)) ?? [];
  const beforeHistory = selected !== undefined && history?.historyStart !== undefined && selected < history.historyStart;
  const missing = gaps.length > 0 || beforeHistory;
  const rows = series.map(s => {
    const point = missing || bucket === undefined ? undefined : s.points.find(p => p.time === bucket);
    return `${s.label}: ${point?.text ?? "No sample"}`;
  });
  const when = bucket === undefined ? "" : `${dayMonHm(bucket).split(" ").slice(0, 2).join(" ")} ${hms(bucket)} UTC`;
  const bucketEnd = bucket === undefined ? "" : `${hms(Math.min(end, bucket + step))} UTC`;
  const text = `${when}${stepMs ? ` to ${bucketEnd} · bucket` : " · recorded sample"}. ${missing ? "Not collected. " : ""}${bucketNote ? bucketNote + ". " : ""}${rows.join("; ")}`;
  const move = (clientX: number, element: HTMLElement) => {
    const box = element.getBoundingClientRect();
    if (box.width > 0) setCursor(start + Math.max(0, Math.min(1, (clientX - box.left) / box.width)) * (end - start));
  };
  return <div className="chart-inspection-wrap"><div ref={anchor} className="chart-inspection" role="slider" tabIndex={0} aria-label={`Inspect ${label}`} aria-valuemin={start} aria-valuemax={end} aria-valuenow={selected ?? start} aria-valuetext={selected === undefined ? "Focus, then use arrow keys to inspect time and values" : text} aria-description={[copy?"Use arrow keys to select time, then C to copy as prompt":"",interval?"Shift and arrow keys select an interval; Enter opens actions":""].filter(Boolean).join(". ")||undefined} aria-describedby={selected === undefined ? undefined : id}
    onPointerMove={e => {interval?.move(e);move(e.clientX,e.currentTarget)}} onPointerDown={e => {interval?.down(e);move(e.clientX,e.currentTarget)}} onPointerUp={e=>interval?.up(e)} onPointerCancel={()=>interval?.cancel()} onLostPointerCapture={()=>interval?.cancel()} onClickCapture={e=>interval?.click(e)} onDragStart={e=>{if(interval)e.preventDefault()}}
    onPointerLeave={e => { if (e.pointerType !== "touch") setCursor(undefined); }}
    onFocus={() => setCursor(current => current ?? start)} onBlur={() => setCursor(undefined)}
    onKeyDown={e => {
      if(interval?.key(e,selected??start,step))return;
      if (e.key.toLowerCase() === "c" && !e.ctrlKey && !e.metaKey && copy) { e.preventDefault(); copy(); return; }
      const current = selected ?? start;
      const next = e.key === "Home" ? start : e.key === "End" ? end : e.key === "ArrowRight" || e.key === "ArrowUp" ? current + step : e.key === "ArrowLeft" || e.key === "ArrowDown" ? current - step : undefined;
      if (e.key === "Escape") { e.preventDefault(); setCursor(undefined); }
      if (next !== undefined) { e.preventDefault(); setCursor(Math.min(end, Math.max(start, next))); }
    }}>
    {children}
    {interval?.highlight&&<span role="img" aria-label={`Selected chart interval ${new Date(interval.highlight.start).toISOString()} to ${new Date(interval.highlight.end).toISOString()}`} className="chart-selected-interval" style={{left:`${Math.max(0,Math.min(100,(interval.highlight.start-start)/(end-start)*100))}%`,width:`${Math.max(0,Math.min(end,interval.highlight.end)-Math.max(start,interval.highlight.start))/(end-start)*100}%`}}/>}
    {selected !== undefined && !interval?.active && <>
      <span className="chart-cursor" aria-hidden="true" style={{ left: `${(selected - start) / (end - start || 1) * 100}%` }} />
      <ChartReadout anchor={anchor} fraction={(selected-start)/(end-start||1)} id={id} contentKey={text}><b>{when}</b><span>{stepMs ? `Bucket to ${bucketEnd}` : "Recorded sample"}</span>{bucketNote && <span>{bucketNote}</span>}{missing && <span>Not collected</span>}{rows.map((r, i) => <span key={i}>{r}</span>)}</ChartReadout>
    </>}
  </div>{interval&&<button type="button" className="btn chart-interval-trigger" aria-label={`Select interval for ${label}`} title="Select interval" onClick={interval.picker}><svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M2 3v10M14 3v10M2 8h12m-9-3L2 8l3 3m6-6 3 3-3 3" fill="none" stroke="currentColor"/></svg></button>}{copy && (actionTarget===undefined?<InvestigationButton selection={selection()} floating />:actionTarget?createPortal(<InvestigationButton selection={selection()} floating />,actionTarget):null)}</div>;
}

// One cursor pattern for compact and expanded charts. Values belong to
// recorded buckets; the cursor never interpolates or carries a value across a gap.
import { useId, useState, type ReactNode } from "react";
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

export function ChartInspection({ label, start, end, stepMs, bucketNote, series, children }: {
  label: string; start: number; end: number; stepMs?: number; bucketNote?: string; series: InspectionSeries[]; children: ReactNode;
}) {
  const [cursor, setCursor] = useState<number>();
  const id = useId();
  const history = usePageHistory();
  const step = inspectionStep(series, stepMs);
  const selected = cursor === undefined ? undefined : Math.min(end, Math.max(start, cursor));
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
  return <div className="chart-inspection" role="slider" tabIndex={0} aria-label={`Inspect ${label}`} aria-valuemin={start} aria-valuemax={end} aria-valuenow={selected ?? start} aria-valuetext={selected === undefined ? "Focus, then use arrow keys to inspect time and values" : text} aria-describedby={selected === undefined ? undefined : id}
    onPointerMove={e => move(e.clientX, e.currentTarget)} onPointerDown={e => move(e.clientX, e.currentTarget)}
    onPointerLeave={e => { if (e.pointerType !== "touch") setCursor(undefined); }}
    onFocus={() => setCursor(start)} onBlur={() => setCursor(undefined)}
    onKeyDown={e => {
      const current = selected ?? start;
      const next = e.key === "Home" ? start : e.key === "End" ? end : e.key === "ArrowRight" || e.key === "ArrowUp" ? current + step : e.key === "ArrowLeft" || e.key === "ArrowDown" ? current - step : undefined;
      if (e.key === "Escape") { e.preventDefault(); setCursor(undefined); }
      if (next !== undefined) { e.preventDefault(); setCursor(Math.min(end, Math.max(start, next))); }
    }}>
    {children}
    {selected !== undefined && <>
      <span className="chart-cursor" aria-hidden="true" style={{ left: `${(selected - start) / (end - start || 1) * 100}%` }} />
      <div id={id} role="tooltip" className="chart-readout"><b>{when}</b><span>{stepMs ? `Bucket to ${bucketEnd}` : "Recorded sample"}</span>{bucketNote && <span>{bucketNote}</span>}{missing && <span>Not collected</span>}{rows.map((r, i) => <span key={i}>{r}</span>)}</div>
    </>}
  </div>;
}

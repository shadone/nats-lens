import { useId } from "react";
import { useDataHistory } from "../api/queries";
import { usePageHistory } from "../components/HistoryNote";
import { gapName, stripSegments } from "../lib/historyView";
import { useTimeRange } from "../lib/useTimeRange";
import { PRESETS, presetMs, type RangeSpec } from "../lib/timeRange";
import { useReportStale } from "../components/StaleContext";
import type { HistoryGap } from "../api/history";

/** Share the page key for normal charts; wider fixed windows share a bounded
 * domain key (seven-day live charts use the stable 7d key, never a ticking key). */
function chartRange(spec: RangeSpec, start: number, end: number): RangeSpec {
  if (spec.kind === "fixed") {
    return start >= spec.from && end <= spec.to ? spec : { kind: "fixed", from: start, to: end };
  }
  const span = Math.max(end - start, presetMs(spec.preset));
  const preset = PRESETS.find((p) => p.ms >= span);
  return preset ? { kind: "live", preset: preset.id } : { kind: "fixed", from: start, to: end };
}

function QueriedGaps({ start, end, width, height }: Frame) {
  const { spec } = useTimeRange();
  const h = usePageHistory();
  const q = useDataHistory(chartRange(spec, start, end));
  useReportStale(q);
  const d = q.data;
  return <>
    <GapMarks start={start} end={end} width={width} height={height} gaps={d?.gaps ?? []} observedStart={d?.rangeStart} observedEnd={d?.rangeEnd} historyStart={h?.historyStart} live={h?.live ?? false} />
    {(q.error || q.isPending) && <text x="2" y="8" className="dh-chart-status" role="status">{q.error ? "History gaps unavailable" : "History gaps loading"}</text>}
  </>;
}
interface Frame { start: number; end: number; width: number; height: number }

export function HistoryGaps(props: Frame) {
  const h = usePageHistory();
  if (!h) return null;
  return h.gaps ? <GapMarks {...props} gaps={h.gaps} observedStart={h.start} observedEnd={h.end} historyStart={h.historyStart} live={h.live} /> : <QueriedGaps {...props} />;
}

function GapMarks({ start, end, width, height, gaps, live, historyStart, observedStart, observedEnd }: Frame & { gaps: HistoryGap[]; live: boolean; historyStart?: number; observedStart?: number; observedEnd?: number }) {
  const id = useId();
  // Preserve the answer's observation bounds. An unset end means open at
  // THAT range end, not open through any later chart end or refresh failure.
  const bounded = gaps.flatMap((g) => {
    const from = Math.max(g.start ?? Infinity, observedStart ?? start);
    const to = Math.min(g.end ?? observedEnd ?? end, observedEnd ?? end);
    if (to <= from) return [];
    return [{ ...g, start: from, end: g.end === undefined && to >= end ? undefined : to }];
  });
  // A fixed seven-day chart may begin before the page query. LensInfo's
  // history start covers that unobserved prefix without another request.
  const shown = historyStart !== undefined && historyStart > start
    ? [...bounded.filter((g) => g.kind !== "beforeHistory"), { kind: "beforeHistory" as const, start, end: historyStart, eventId: "", connection: "" }]
    : bounded;
  const segments = stripSegments({ gaps: shown, rangeStart: start, rangeEnd: end }).filter((s) => s.width > 0);
  return <g>
    <defs><pattern id={id} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="6" height="6" className="dh-gap-bg" /><line x1="0" y1="0" x2="0" y2="6" className="dh-gap-line" /></pattern></defs>
    {segments.map((s, i) => {
      const name = gapName(s.gap, end, live);
      const limit = s.gap.kind === "beforeHistory" ? width : Math.min(1, ((observedEnd ?? end) - start) / (end - start)) * width;
      return <rect className="dh-chart-gap" key={i} x={s.left / 100 * width} y="0" width={Math.min(limit - s.left / 100 * width, Math.max(1.5, s.width / 100 * width))} height={height} fill={`url(#${id})`} role="img" aria-label={name}><title>{name}</title></rect>;
    })}
  </g>;
}

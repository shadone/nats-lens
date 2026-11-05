import { useId } from "react";
import { useDataHistory } from "../api/queries";
import { usePageHistory } from "../components/HistoryNote";
import { gapName, stripSegments } from "../lib/historyView";
import { useTimeRange } from "../lib/useTimeRange";
import type { HistoryGap } from "../api/history";

/** Mounted only by charts inside the page layout: all charts share one cached request. */
function QueriedGaps({ start, end, width, height }: Frame) {
  const { spec } = useTimeRange();
  const q = useDataHistory(spec);
  const h = usePageHistory();
  // Placeholder data can belong to an earlier range. Clip to this chart's full axis.
  return <GapMarks start={start} end={end} width={width} height={height} gaps={q.data?.gaps ?? []} live={h?.live ?? false} />;
}
interface Frame { start: number; end: number; width: number; height: number }

export function HistoryGaps(props: Frame) {
  const h = usePageHistory();
  if (!h) return null;
  return h.gaps ? <GapMarks {...props} gaps={h.gaps} live={h.live} /> : <QueriedGaps {...props} />;
}

function GapMarks({ start, end, width, height, gaps, live }: Frame & { gaps: HistoryGap[]; live: boolean }) {
  const id = useId();
  const segments = stripSegments({ gaps, rangeStart: start, rangeEnd: end }).filter((s) => s.width > 0);
  return <g>
    <defs><pattern id={id} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(45)"><rect width="6" height="6" className="dh-gap-bg" /><line x1="0" y1="0" x2="0" y2="6" className="dh-gap-line" /></pattern></defs>
    {segments.map((s, i) => {
      const name = gapName(s.gap, end, live);
      return <rect className="dh-chart-gap" key={i} x={s.left / 100 * width} y="0" width={s.width / 100 * width} height={height} fill={`url(#${id})`} role="img" aria-label={name}><title>{name}</title></rect>;
    })}
  </g>;
}

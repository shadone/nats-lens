// The hatched gaps over a chart: each time without collections, and the
// time before the history, as a hatched stretch with its label as a
// tooltip and as its accessible name. Without gaps the chart is returned
// as it is.
import type { ReactNode } from "react";
import { serverNow } from "../lib/clock";
import { gapMarks, type ChartGaps } from "../lib/historyNotes";

export function WithGaps({ gaps, start, end, children }: { gaps?: ChartGaps; start: number; end: number; children: ReactNode }) {
  const marks = gapMarks(gaps, start, end, serverNow());
  if (marks.length === 0) return children;
  return (
    <div className="gap-wrap">
      {children}
      <div className="gap-layer">
        {marks.map((m) => (
          <span key={m.key} className="gap-seg gap-hatch" role="img" aria-label={m.name} title={m.name} style={{ left: `${m.left}%`, width: `${m.width}%` }} />
        ))}
      </div>
    </div>
  );
}

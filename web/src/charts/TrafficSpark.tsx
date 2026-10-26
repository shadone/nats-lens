// An application's messages in and out per hour: in as a solid accent
// line, out as a dashed one, on one scale. An hour without a measurement
// breaks the line, so a gap never reads as zero; a measured hour between
// two gaps is a dot.
import type { AppHour } from "../api/applications";

const W = 140;
const H = 24;

type ValueOf = (h: AppHour) => number | undefined;

/** Runs of consecutive set hours as point lists, x by hour index, y on the shared max. */
export function segments(hours: AppHour[], pick: ValueOf, max: number): { x: number; y: number }[][] {
  const out: { x: number; y: number }[][] = [];
  let run: { x: number; y: number }[] = [];
  const step = hours.length > 1 ? W / (hours.length - 1) : 0;
  hours.forEach((h, i) => {
    const v = pick(h);
    if (v === undefined) {
      if (run.length) out.push(run);
      run = [];
      return;
    }
    run.push({ x: hours.length > 1 ? i * step : W / 2, y: H - 2 - (max > 0 ? (v / max) * (H - 5) : 0) });
  });
  if (run.length) out.push(run);
  return out;
}

function Series({ runs, cls }: { runs: { x: number; y: number }[][]; cls: string }) {
  return (
    <>
      {runs.map((r) =>
        r.length === 1 ? (
          <circle key={r[0].x} className={cls} cx={r[0].x.toFixed(1)} cy={r[0].y.toFixed(1)} r="1.4" />
        ) : (
          <polyline key={r[0].x} className={cls} points={r.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")} vectorEffect="non-scaling-stroke" />
        ),
      )}
    </>
  );
}

export function TrafficSpark({ hours, label }: { hours: AppHour[]; label: string }) {
  const vals = hours.flatMap((h) => [h.inMsgs, h.outMsgs]).filter((v): v is number => v !== undefined);
  if (vals.length === 0) {
    return (
      <svg className="ap-spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
        <line className="ap-spark-none" x1="0" x2={W} y1={H - 2} y2={H - 2} strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />
      </svg>
    );
  }
  const max = Math.max(...vals);
  return (
    <svg className="ap-spark" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
      <Series runs={segments(hours, (h) => h.outMsgs, max)} cls="ap-spark-out" />
      <Series runs={segments(hours, (h) => h.inMsgs, max)} cls="ap-spark-in" />
    </svg>
  );
}

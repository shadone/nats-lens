// Events over the range as stacked bars: warnings on top, other notable
// events below them, routine events at the bottom. The legend draws each
// bar style.
import type { HistogramBucket } from "../api/events";
import { eventWhen, fmtInt } from "../lib/format";

const KINDS = [
  { k: "warning", label: "warnings" },
  { k: "notable", label: "notable" },
  { k: "routine", label: "routine" },
] as const;

const W = 10;
const GAP = 2;
const H = 64;

function Swatch({ k }: { k: (typeof KINDS)[number]["k"] }) {
  return (
    <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
      <rect className={`hb-${k}`} x="0" y="0" width="10" height="10" rx="2" />
    </svg>
  );
}

export function EventHistogram({ buckets, start, end, live, routineHidden }: {
  buckets: HistogramBucket[];
  start: number;
  end: number;
  live: boolean;
  routineHidden: boolean;
}) {
  const totals = buckets.reduce((t, b) => ({ warning: t.warning + b.warning, notable: t.notable + b.notable, routine: t.routine + b.routine }), { warning: 0, notable: 0, routine: 0 });
  const max = Math.max(1, ...buckets.map((b) => b.warning + b.notable + b.routine));
  const width = Math.max(1, buckets.length) * W;
  const label = `Events over the range: ${fmtInt(totals.warning)} warnings, ${fmtInt(totals.notable)} notable, ${fmtInt(totals.routine)} routine`;
  const h = (n: number) => (n / max) * H;
  return (
    <section className="card ev-hist" aria-label="Events over time">
      <svg className="ev-hist-bars" viewBox={`0 0 ${width} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
        {buckets.map((b, i) => {
          const x = i * W + GAP / 2;
          const r = h(b.routine);
          const n = h(b.notable);
          const w = h(b.warning);
          return (
            <g key={b.start}>
              <title>{`${eventWhen(b.start, end)} UTC: ${b.warning} warnings, ${b.notable} notable, ${b.routine} routine`}</title>
              {/* A transparent bar keeps the whole column hoverable. */}
              <rect x={x} y={0} width={W - GAP} height={H} fill="transparent" />
              {r > 0 && <rect className="hb-routine" x={x} y={H - r} width={W - GAP} height={r} />}
              {n > 0 && <rect className="hb-notable" x={x} y={H - r - n} width={W - GAP} height={n} />}
              {w > 0 && <rect className="hb-warning" x={x} y={H - r - n - w} width={W - GAP} height={w} />}
            </g>
          );
        })}
      </svg>
      <div className="ev-hist-axis mono" aria-hidden="true">
        <span>{eventWhen(start, end)}</span>
        <span>{live ? "now" : eventWhen(end, end)}</span>
      </div>
      <ul className="ev-hist-legend" aria-label="Histogram legend">
        {KINDS.map(({ k, label: l }) => (
          <li key={k}>
            <Swatch k={k} /> {l}
            {k === "routine" && routineHidden ? " (hidden below)" : ""}
          </li>
        ))}
      </ul>
    </section>
  );
}

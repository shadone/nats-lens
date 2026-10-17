// One system's card on the Fleet page, its status strip and the strip's
// legend. The state is a shape and a word as well as a colour.
import type { Level, SystemSummary } from "../api/model";
import { CARD_STATE_LABEL, cardState, freshnessText, headline, type CardState } from "../lib/fleet";
import { hm } from "../lib/format";
import { bucketLabel } from "../lib/timeRange";
import { cellRuns, runsText } from "../lib/timelineModel";
import { LEVEL_LABEL } from "../lib/view";
import { Num, RLink } from "./common";
import { HealthIcon } from "./Severity";
import { CELL_FILL } from "./StatusTimeline";

function StateMark({ state }: { state: CardState }) {
  if (state === "notcollecting")
    return (
      <svg className="hi" viewBox="0 0 14 14" aria-hidden="true">
        <circle cx="7" cy="7" r="5.5" style={{ fill: "none", stroke: "var(--text-muted)" }} strokeWidth="1.5" />
        <path d="M3.2 10.8l7.6-7.6" style={{ stroke: "var(--text-muted)" }} strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    );
  if (state === "unavailable")
    return (
      <svg className="hi" viewBox="0 0 14 14" aria-hidden="true">
        <rect x="1.5" y="1.5" width="11" height="11" rx="2" style={{ fill: "none", stroke: "var(--text-muted)" }} strokeWidth="1.4" strokeDasharray="2 1.6" />
        <path d="M7 9.6v.5" style={{ stroke: "var(--text-muted)" }} strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    );
  return (
    <span aria-hidden="true">
      <HealthIcon level={state} />
    </span>
  );
}

const SW = 480;

/** A cell's fill; a no-data cell is outlined too, so it shows against the card. */
function cellStyle(lv: Level) {
  return lv === "nodata" ? { fill: CELL_FILL[lv], stroke: "var(--border)", strokeWidth: 1 } : { fill: CELL_FILL[lv] };
}

/** The worst status per bucket, oldest first, described as text for a screen reader. */
export function FleetStrip({ cells, start, bucketMs }: { cells: Level[]; start: number; bucketMs: number }) {
  const cw = SW / Math.max(1, cells.length);
  return (
    <svg className="fc-strip" viewBox={`0 0 ${SW} 14`} preserveAspectRatio="none" role="img" aria-label={`Status per ${bucketLabel(bucketMs)}: ${runsText(cellRuns(cells, start, bucketMs))}`}>
      {cells.map((lv, i) => (
        <rect key={i} x={(i * cw + 0.6).toFixed(2)} y="0" width={Math.max(0.5, cw - 1.6).toFixed(2)} height="14" rx="1.5" style={cellStyle(lv)} vectorEffect="non-scaling-stroke" data-level={lv}>
          <title>{`${hm(start + i * bucketMs)} ${LEVEL_LABEL[lv].toLowerCase()}`}</title>
        </rect>
      ))}
    </svg>
  );
}

const LEGEND_ALWAYS: Level[] = ["ok", "info", "warning", "critical"];

/** Draws each strip cell it names; muted and no data only when a strip has them. */
export function StripLegend({ used, bucketMs }: { used: Set<Level>; bucketMs?: number }) {
  const levels = [...LEGEND_ALWAYS, ...(["muted", "nodata"] as const).filter((l) => used.has(l))];
  return (
    <div className="fleet-key">
      <span>Strip{bucketMs ? `, per ${bucketLabel(bucketMs)}` : ""}:</span>
      <ul aria-label="Strip legend">
        {levels.map((l) => (
          <li key={l}>
            <svg viewBox="0 0 8 12" aria-hidden="true">
              <rect x="0.5" y="0.5" width="7" height="11" rx="1.5" style={cellStyle(l)} />
            </svg>
            {LEVEL_LABEL[l].toLowerCase()}
          </li>
        ))}
      </ul>
    </div>
  );
}

function Count({ v, of, label }: { v?: number; of?: number; label: string }) {
  const short = v !== undefined && of !== undefined && v < of;
  return (
    <div className={`fc-n${short ? " bad" : ""}`}>
      <b>
        <Num v={v} />
      </b>
      {of !== undefined && <small> of {of}</small>}
      <span className="k">{label}</span>
    </div>
  );
}

interface Props {
  s: SystemSummary;
  /** The range is live: the strip ends now and freshness ticks. */
  live: boolean;
  /** The strip's left end in words: "24h ago", or a time for a fixed range. */
  startLabel: string;
  now: number;
}

export function FleetCard({ s, live, startLabel, now }: Props) {
  const state = cardState(s);
  const h = headline(s, now);
  const start = s.stripStart;
  const bucket = s.stripBucketMs;
  return (
    <li className={`card fc ${state}`}>
      <RLink to={`/?system=${encodeURIComponent(s.id)}`} className="fc-link">
        <div className="fc-h">
          <h2 className="fc-name">{s.name}</h2>
          {s.environment && <span className="fc-env">{s.environment}</span>}
          <span className={`fc-state ${state}`}>
            <StateMark state={state} />
            {CARD_STATE_LABEL[state]}
          </span>
        </div>
        <p className="fc-head">
          {h.text}
          {h.more > 0 && <span className="fc-more"> +{h.more} more</span>}
        </p>
        {state !== "unavailable" && (
          <>
            <div className="fc-counts">
              <Count v={s.serversAnswering ?? s.servers} of={s.serversAnswering !== undefined ? s.servers : undefined} label="servers" />
              <Count v={s.clusters} label="clusters" />
              <Count v={s.streams} label="streams" />
              <Count v={s.connections} label="connections" />
            </div>
            {s.strip.length > 0 && start !== undefined && bucket ? <FleetStrip cells={s.strip} start={start} bucketMs={bucket} /> : null}
            <div className="fc-foot">
              <span>{startLabel}</span>
              <span>{freshnessText(s, live, now)}</span>
            </div>
          </>
        )}
      </RLink>
    </li>
  );
}

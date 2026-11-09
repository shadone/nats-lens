// What happened over the range, in three lanes on one clock: Kubernetes
// events with the drain and upgrade windows, the worst finding state of
// any check, and the NATS events. "Each check" opens the full status
// timeline, one row per check, in the same card.
import { useRef, useState, type CSSProperties } from "react";
import type { LaneEvent, Timeline } from "../api/model";
import { hm } from "../lib/format";
import { findingRuns } from "../lib/storyLanes";
import { useWidth } from "../lib/useWidth";
import { axisTicks, isDeployment, runsText, zoomWindow } from "../lib/timelineModel";
import { RLink } from "./common";
import { StatusTimeline, trackSpan } from "./StatusTimeline";
import {InfoTip} from "./MetricBox";
import {TimelineInvestigationProvider,TimelineFindingRange} from "./TimelineInvestigation";
import {TimelineMark, EventPreview, EventDetails, WindowPreview, timelineTime} from "./TimelineMark";
import {LEVEL_LABEL} from "../lib/view";
import { ZoomStrip } from "./ZoomStrip";

interface Props {
  tl: Timeline;
  end: number;
  live: boolean;
}

/** "2 checks fired": the checks with any cell other than ok or no data, info included. */
function firedNote(n: number): string {
  return `${n} ${n === 1 ? "check" : "checks"} fired`;
}

function pct(v: number): string {
  return `${(Math.min(1, Math.max(0, v)) * 100).toFixed(3)}%`;
}

function Ticks({ events, frac, cls }: { events: LaneEvent[]; frac: (t: number) => number; cls: (e: LaneEvent) => string }) {
  return (
    <>
      {events.map((e) => (
        <TimelineMark key={e.id} className={`sl-tick ${cls(e)}`} style={{left:pct(frac(e.time))}} label={`${e.source === "kubernetes" ? "Kubernetes" : "NATS"} event: ${e.label}`} preview={<EventPreview event={e}/>} details={<EventDetails event={e}/>}/>
      ))}
    </>
  );
}

function natsClass(e: LaneEvent): string {
  return e.severity === "critical" ? "crit" : e.severity === "warning" ? "warn" : "nats";
}

/** The marks the lanes draw, each drawn as in the lanes. */
function LaneLegend({ windows }: { windows: boolean }) {
  const item = (cls: string, label: string) => (
    <li key={cls}>
      <span className={`sl-key ${cls}`} aria-hidden="true" />
      {label}
    </li>
  );
  return (
    <ul className="sl-legend" aria-label="Lane legend">
      {item("sl-tick k8s", "Kubernetes event")}
      {item("sl-tick k8s deploy", "deployment")}
      {windows && item("sl-win", "drain or upgrade window")}
      {item("sl-run critical", "critical")}
      {item("sl-run warning", "warning")}
      {item("sl-run info", "info")}
      {item("sl-run muted", "muted")}
      {item("sl-tick nats", "NATS event")}
      {item("sl-tick warn", "NATS warning")}
    </ul>
  );
}

export function StoryLanes(props:Props) { return <TimelineInvestigationProvider><StoryLaneContents {...props}/></TimelineInvestigationProvider>; }

function StoryLaneContents({ tl, end, live }: Props) {
  const [checks, setChecks] = useState(false);
  const [asTable, setAsTable] = useState(false);
  const span = trackSpan(tl, end);
  const axis = useRef<HTMLDivElement>(null);
  const axisW = useWidth(axis);
  const frac = (t: number) => (t - tl.start) / span;
  const runs = findingRuns(tl.rows, tl.start, tl.bucketMs, tl.bucketCount).filter((r) => r.level !== "nodata");
  const deploys = tl.kubernetes.filter(isDeployment).length;
  const natsWarn = tl.nats.filter((e) => e.severity === "warning" || e.severity === "critical").length;
  const win = zoomWindow(tl.windows);
  const windows = tl.windows.map((w) => (
    <TimelineMark key={w.start} className="sl-win" style={{left:pct(frac(w.start)),width:pct(frac(w.end)-frac(w.start))}} label={`Window: ${w.label}`} preview={<WindowPreview {...w}/>}/>
  ));
  const k8sName = `Kubernetes: ${tl.kubernetes.length} events${deploys ? `, ${deploys} deployments` : ""}${tl.windows.map((w) => `, ${w.label} ${hm(w.start)}–${hm(w.end)}`).join("")}`;
  const lanes = [
    {
      id: "k8s",
      name: "Kubernetes",
      note: tl.kubernetes.length ? `${tl.kubernetes.length} events` : "no events",
      label: k8sName,
      marks: (
        <>
          {windows}
          <Ticks events={tl.kubernetes} frac={frac} cls={(e) => `k8s${isDeployment(e) ? " deploy" : ""}`} />
        </>
      ),
      more: "/events?src=kubernetes",
    },
    {
      id: "findings",
      name: "Findings",
      note: tl.rows.length ? firedNote(new Set(tl.rows.filter((r) => r.cells.some((c) => c !== "ok" && c !== "nodata")).map((r) => r.checkCode)).size) : "no check ran",
      label: `Findings: ${runs.length ? runsText(runs) : "ok throughout"}`,
      marks: (
        <>
          {runs.map((r) => (
            <TimelineMark key={r.from} className={`sl-run ${r.level}`} style={{left:pct(frac(r.from)),width:pct(frac(r.to)-frac(r.from))}} label={`Finding state: ${LEVEL_LABEL[r.level]}`} details={<TimelineFindingRange readEpoch={tl.findingReadEpoch} from={r.from} end={r.to}/>} preview={<><strong>{LEVEL_LABEL[r.level]} finding state</strong><div>{timelineTime(r.from)} – {timelineTime(r.to)}</div><div>Bucket boundaries, not exact onset or resolution.</div>{tl.rows.filter(row=>row.cells.some((lv,i)=>tl.start+i*tl.bucketMs<r.to&&tl.start+(i+1)*tl.bucketMs>r.from&&lv!=="ok"&&lv!=="nodata")).map(row=><div key={row.key}>{row.checkCode}: {row.checkName}</div>)}</>}/>
          ))}
        </>
      ),
      more: "/findings",
    },
    {
      id: "nats",
      name: "Events",
      note: tl.nats.length ? `${tl.nats.length} NATS${natsWarn ? `, ${natsWarn} warnings` : ""}` : "no NATS events",
      label: `NATS events: ${tl.nats.length}${natsWarn ? `, ${natsWarn} warnings` : ""}`,
      marks: <Ticks events={tl.nats} frac={frac} cls={natsClass} />,
      more: "/events?src=nats",
    },
  ];
  const style = { "--buckets": tl.bucketCount } as CSSProperties;
  return (
    <div className="story" style={style}>
      <div className="sl-grid">
        {lanes.map((l) => (
          <div className="sl-row" key={l.id}>
            <div className="sl-lab">
              <RLink to={l.more} className="x sl-name">
                {l.name}
              </RLink>
              <small>{l.note}</small>
            </div>
            <div className="sl-track" role="group" aria-label={l.label}>
              <span className="sl-base" />
              {l.marks}
            </div>
          </div>
        ))}
        <div className="sl-row sl-axis" aria-hidden="true">
          <div />
          <div className="sl-ticks" ref={axis}>
            {axisTicks(tl.start, tl.start + span, live, axisW || undefined).map((t) => (
              <span key={`${t.frac}`} className={t.edge ? "edge-r" : undefined} style={{ left: pct(t.frac) }}>
                {t.label}
              </span>
            ))}
          </div>
        </div>
      </div>
      <div className="sl-foot">
        <LaneLegend windows={tl.windows.length > 0} />
        <InfoTip label="Timeline lanes" text="Events are observed timestamps; coloured finding boxes show the worst check state in each time bucket. A box is not one incident or an exact onset. Shaded windows are inferred context, not proven causes. Hover or focus a mark to inspect it; activate it to open its evidence. Missing or truncated captures limit what the Timeline can show."/>
        <span className="right">
          {checks && (
            <button type="button" className="chip" aria-pressed={asTable} onClick={() => setAsTable((t) => !t)}>
              {asTable ? "Show chart" : "Show as table"}
            </button>
          )}
          <button type="button" className="chip" aria-expanded={checks} onClick={() => setChecks((c) => !c)}>
            {checks ? "Hide each check" : "Each check"}
          </button>
        </span>
      </div>
      {checks && (
        <div className="sl-checks tl">
          {tl.rows.length === 0 && tl.kubernetes.length === 0 ? (
            <p className="muted-note">No check has reported in this range. The timeline fills in once checks run after each collection.</p>
          ) : (
            <StatusTimeline tl={tl} end={end} live={live} asTable={asTable} />
          )}
        </div>
      )}
      {win && !(checks && asTable) && <ZoomStrip tl={tl} win={win} />}
    </div>
  );
}

import {timelineCaptureLimits} from "./TimelineCaptureNotice";
// What happened over the range, in lanes on one clock: Kubernetes events
// with the drain and upgrade windows, server restarts and announcements,
// leader elections, the worst finding state of any check, client
// connection counts, and the other NATS events. "Each check" opens the
// full status timeline, one row per check, in the same card.
import { useEffect, useId, useRef, useState, type CSSProperties, type PointerEvent } from "react";
import { useChartIntervalInteraction } from "../charts/ChartIntervals";
import { useSharedCursor } from "../charts/sharedCursor";
import type { CaptureGap, LaneEvent, Range, Timeline } from "../api/model";
import { hm } from "../lib/format";
import { findingRuns } from "../lib/storyLanes";
import { useWidth } from "../lib/useWidth";
import { axisTicks, isDeployment, runsText, zoomWindow } from "../lib/timelineModel";
import {Link} from "react-router";
import {useSystem} from "../lib/useSystem";
import { StatusTimeline, trackSpan } from "./StatusTimeline";
import {InfoTip} from "./MetricBox";
import {TimelineInvestigationProvider,TimelineFindingRange} from "./TimelineInvestigation";
import {TimelineMark, WindowPreview, timelineTime} from "./TimelineMark";
import {LEVEL_LABEL} from "../lib/view";
import {TimelineEventTicks} from "./TimelineEventTicks";
import {TimelineWindowEvidence} from "./TimelineEventEvidence";
import {findingHistoryPath} from "../lib/findingHistory";
import {evidenceRange,evidenceQuery} from "../lib/timelineEvidence";
import { ZoomStrip } from "./ZoomStrip";
import {TimelineClientLane,clientCountsText,clientNoteText} from "./TimelineClientLane";
import {CLIENT_LANE_KINDS,cellTotal} from "../lib/clientLane";
import {TimelineCaptureGap,gapConnectionText,gapSpanText} from "./TimelineCaptureGap";

interface Props {
  tl: Timeline;
  end: number;
  live: boolean;
}

/** "2 checks fired": the checks with any cell other than ok or no data, info included. */
function firedNote(n: number): string {
  return `${n} ${n === 1 ? "check" : "checks"} fired`;
}

const plural = (n: number, one: string) => `${n} ${n === 1 ? one : `${one}s`}`;

function pct(v: number): string {
  return `${(Math.min(1, Math.max(0, v)) * 100).toFixed(3)}%`;
}

function natsClass(e: LaneEvent): string {
  return e.severity === "critical" ? "crit" : e.severity === "warning" ? "warn" : "nats";
}

/** The marks the lanes draw, each drawn as in the lanes. */
function LaneLegend({ windows, subjects, gaps }: { windows: boolean; subjects: boolean; gaps: boolean }) {
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
      {item("sl-run nodata", "no data")}
      {subjects && item("sl-tick srv", "server restart or event")}
      {subjects && item("sl-tick lead", "leader election")}
      {subjects && item("sl-density", "client events")}
      {subjects && item("sl-density drops", "client drops")}
      {gaps && item("sl-gap", "event capture stopped")}
      {item("sl-tick nats", "NATS event")}
      {item("sl-tick warn", "NATS warning")}
    </ul>
  );
}

export function StoryLanes(props:Props) { return <TimelineInvestigationProvider><StoryLaneContents {...props}/></TimelineInvestigationProvider>; }

function StoryLaneContents({ tl, end, live }: Props) {
  const system=useSystem();
  const [checks, setChecks] = useState(false);
  const [asTable, setAsTable] = useState(false);
  const span = trackSpan(tl, end);
  const axis = useRef<HTMLDivElement>(null);
  const axisW = useWidth(axis);
  const frac = (t: number) => (t - tl.start) / span;
  const grid = useRef<HTMLDivElement>(null);
  // The Overview's shared moment: one line over every lane, by a CSS
  // variable on the grid, also at the moment the pointer marks here.
  const laneId = useId();
  const shared = useSharedCursor(laneId, tl.start, tl.start + span, (f) => {
    const el = grid.current;
    if (!el) return;
    if (f === undefined) el.removeAttribute("data-cursor");
    else {
      el.style.setProperty("--cursor-x", `${(f * 100).toFixed(3)}%`);
      el.setAttribute("data-cursor", "");
    }
  }, { own: true });
  // Dragging across the lanes selects an interval, as on the charts.
  const interval = useChartIntervalInteraction({ label: "Timeline", start: tl.start, end: tl.start + span, enabled: true });
  const point = (e: PointerEvent<HTMLDivElement>) => {
    const box = e.currentTarget.getBoundingClientRect();
    if (!shared || box.width <= 0) return;
    shared(e.pointerType === "touch" || interval?.active ? undefined : tl.start + Math.max(0, Math.min(1, (e.clientX - box.left) / box.width)) * span);
  };
  const selected = interval?.highlight;
  // A drag or an interval dialog takes the lanes' line away.
  useEffect(() => { if (interval?.active) shared?.(undefined); }, [interval?.active, shared]);
  // The track takes no focus, so the dialog of a drag returns it here.
  const picker = useRef<HTMLButtonElement>(null);
 const fixed=tl.evidenceRange??evidenceRange(tl.start,tl.start+span);
  const lanePath=(path:string)=>{const [base,query]=path.split('?'),p=evidenceQuery(system,fixed);new URLSearchParams(query).forEach((v,k)=>p.set(k,v));if(base==='/events'&&tl.eventReadId!==undefined)p.set('eventReadId',tl.eventReadId);return `${base}?${p}`};
  const runs = findingRuns(tl.rows, tl.start, tl.bucketMs, tl.bucketCount);
  const observed=tl.rows.some(row=>row.cells.slice(0,tl.bucketCount).some(level=>level!=="nodata"));
  const deploys = tl.kubernetes.filter(isDeployment).length;
  const natsWarn = tl.nats.filter((e) => e.severity === "warning" || e.severity === "critical").length;
  const win = zoomWindow(tl.windows);
  // A source that does not report the subject lanes draws none of them.
  const subjectLanes = tl.servers !== undefined;
  const servers = tl.servers ?? [], leaders = tl.leaders ?? [];
  const restarts = servers.filter((e) => e.kind === "server.restarted").length;
  const metaElections = leaders.filter((e) => e.kind === "meta.leader_elected").length;
  const clientSum = (tl.clients ?? []).reduce((a, b) => ({connects: a.connects + b.connects, closes: a.closes + b.closes, drops: a.drops + b.drops,
    shutdowns: a.shutdowns + b.shutdowns, authErrors: a.authErrors + b.authErrors, unknownDisconnects: a.unknownDisconnects + b.unknownDisconnects}),
    {connects: 0, closes: 0, drops: 0, shutdowns: 0, authErrors: 0, unknownDisconnects: 0});
  // Spans without event capture: an empty stretch there is not a quiet one.
  // The API's api.CaptureSince: the range's start, or api.CaptureLookback
  // (7 days) before its end when that is earlier.
  const since = Math.min(tl.start, tl.start + span - 7 * 24 * 3600_000);
  const capture = (gaps: (Range | CaptureGap)[], stopped: boolean, lane: 'system' | 'advisory', what: string) => {
    const shown = gaps.filter((g) => g.end > g.start);
    const partial = shown.length > 0 && !stopped;
    return {
      stopped,
      overlay: stopped ? "Event capture stopped for the whole range" : `No ${what} captured since ${timelineTime(since)}`,
      label: stopped ? "event capture stopped for the whole range" : `no ${what} captured since ${timelineTime(since)}`,
      gapNote: partial ? `; capture stopped ${shown.map((g) => gapSpanText(g) + gapConnectionText(g)).join(", ")}` : "",
      noteSuffix: partial ? ", capture gaps" : "",
      marks: shown.map((g) => <TimelineCaptureGap key={`${lane}${g.start}${"connection" in g ? g.connection : ""}`} gap={g} frac={frac} lane={lane}/>),
    };
  };
  const sysGaps = tl.systemCaptureGaps ?? [], advGaps = tl.advisoryCaptureGaps ?? [];
  const covers = (gs: Range[]) => gs.some((g) => g.start <= tl.start && g.end >= tl.start + span);
  const sys = capture(sysGaps, covers(sysGaps), "system", "system event");
  const adv = capture(advGaps, covers(advGaps.filter((g) => !g.connection)), "advisory", "JetStream advisory");
  const windows = tl.windows.map((w) => (
    <TimelineMark key={w.start} className="sl-win" style={{left:pct(frac(w.start)),width:pct(frac(w.end)-frac(w.start))}} label={`Window: ${w.label}`} preview={<WindowPreview {...w}/>} details={<TimelineWindowEvidence window={w} range={fixed} readId={tl.eventReadId}/>}/>
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
          <TimelineEventTicks captureLimits={timelineCaptureLimits(tl,"kubernetes")} events={tl.kubernetes} from={tl.start} end={tl.start+span} width={axisW} range={fixed} readId={tl.eventReadId} className={(e) => `sl-tick k8s${isDeployment(e) ? " deploy" : ""}`} />
        </>
      ),
      more: "/events?src=kubernetes&show=all",
    },
    ...(subjectLanes ? [
    {
      id: "servers",
      name: "Servers",
      note: (servers.length ? [restarts && plural(restarts, "restart"), servers.length - restarts && plural(servers.length - restarts, "event")].filter(Boolean).join(", ") : tl.systemEventsCaptured ? "none in range" : "no restarts seen") + sys.noteSuffix,
      label: `Servers: ${servers.length ? `${plural(restarts, "restart")}, ${plural(servers.length - restarts, "server event")}` : "none in range"}${tl.systemEventsCaptured ? "" : `; ${sys.label}, restarts come from the collections`}${sys.gapNote}`,
      marks: <>
        {!tl.systemEventsCaptured && <span className="sl-none">{sys.overlay}</span>}
        {sys.marks}
        <TimelineEventTicks name="Servers" markLabel={e=>e.kind==="server.restarted"?`Server restart: ${e.label}`:`Server event: ${e.label}`} captureLimits={timelineCaptureLimits(tl,"servers")} events={servers} from={tl.start} end={tl.start+span} width={axisW} range={fixed} readId={tl.eventReadId} className={e=>`sl-tick srv${e.kind==="server.restarted"?" restart":` ${natsClass(e)}`}`} />
      </>,
      more: "/events?kind=server.lameduck,server.shutdown,server.out_of_storage,server.removed&show=all",
    },
    {
      id: "leaders",
      name: "Leaders",
      note: (!tl.jetstreamEventsCaptured ? adv.stopped ? "capture stopped" : "no advisories" : leaders.length ? plural(leaders.length, "election") : "none in range") + adv.noteSuffix,
      label: `Leader elections: ${!tl.jetstreamEventsCaptured ? adv.label : leaders.length ? `${leaders.length}${metaElections ? `, ${metaElections} meta` : ""}` : "none in range"}${adv.gapNote}`,
      marks: <>
        {!tl.jetstreamEventsCaptured && <span className="sl-none">{adv.overlay}</span>}
        {adv.marks}
        <TimelineEventTicks name="Leaders" markLabel={e=>`Leader election: ${e.label}`} captureLimits={timelineCaptureLimits(tl,"leaders")} events={leaders} from={tl.start} end={tl.start+span} width={axisW} range={fixed} readId={tl.eventReadId} className={e=>`sl-tick lead${e.kind==="meta.leader_elected"?" meta":""}`} />
      </>,
      more: "/events?kind=meta.leader_elected,stream.leader_elected,consumer.leader_elected&show=all",
    }] : []),
    {
      id: "findings",
      name: "Findings",
      note: observed ? firedNote(new Set(tl.rows.filter((r) => r.cells.some((c) => c !== "ok" && c !== "nodata")).map((r) => r.checkCode)).size) : "no observed state",
      label: `Findings: ${!observed ? "no observed check state" : runs.length ? runsText(runs) : "OK in observed buckets"}`,
      marks: (
        <>
          {runs.map((r) => (
            <TimelineMark key={r.from} className={`sl-run ${r.level}`} style={{left:pct(frac(r.from)),width:pct(frac(r.to)-frac(r.from))}} label={`Finding state: ${LEVEL_LABEL[r.level]}`} details={<TimelineFindingRange readEpoch={tl.findingReadEpoch} from={r.from} end={r.to}/>} preview={<><strong>{LEVEL_LABEL[r.level]} finding state</strong><div>{timelineTime(r.from)} – {timelineTime(r.to)}</div><div>Bucket boundaries, not exact onset or resolution.</div>{r.level==="nodata"&&<div>No check state was observed in these buckets. Retained occurrences may overlap this interval, but do not prove evaluation in the missing buckets.</div>}{tl.rows.filter(row=>row.cells.some((lv,i)=>tl.start+i*tl.bucketMs<r.to&&tl.start+(i+1)*tl.bucketMs>r.from&&lv!=="ok"&&lv!=="nodata")).map(row=><div key={row.key}>{row.checkCode}: {row.checkName}</div>)}</>}/>
          ))}
        </>
      ),
      more: findingHistoryPath(tl.start,tl.start+span,tl.findingReadEpoch,system,undefined,undefined,fixed),
    },
    ...(subjectLanes ? [{
      id: "clients",
      name: "Clients",
      note: (!tl.systemEventsCaptured ? sys.stopped ? "capture stopped" : "no system events" : cellTotal(clientSum) ? clientNoteText(clientSum) : "none in range") + sys.noteSuffix,
      label: `Client connections: ${!tl.systemEventsCaptured ? sys.label : clientCountsText(clientSum)}${sys.gapNote}`,
      marks: <>
        {!tl.systemEventsCaptured && <span className="sl-none">{sys.overlay}</span>}
        {sys.marks}
        <TimelineClientLane tl={tl} end={tl.start+span} width={axisW}/>
      </>,
      more: `/events?kind=${CLIENT_LANE_KINDS.join(",")}&show=all`,
    }] : []),
    {
      id: "nats",
      name: "Events",
      note: tl.nats.length ? `${tl.nats.length} NATS${natsWarn ? `, ${natsWarn} warnings` : ""}` : "no NATS events",
      label: `NATS events: ${tl.nats.length}${natsWarn ? `, ${natsWarn} warnings` : ""}`,
      marks: <TimelineEventTicks captureLimits={timelineCaptureLimits(tl,"events")} events={tl.nats} from={tl.start} end={tl.start+span} width={axisW} range={fixed} readId={tl.eventReadId} className={e=>`sl-tick ${natsClass(e)}`} />,
      more: "/events?src=nats,findings,collector&show=all",
    },
  ];
  const style = { "--buckets": tl.bucketCount } as CSSProperties;
  return (
    <div className="story" style={style}>
      <div className="sl-grid" ref={grid}>
        {lanes.map((l) => (
          <div className="sl-row" key={l.id}>
            <div className="sl-lab">
              <Link to={lanePath(l.more)} className="x sl-name">
                {l.name}
              </Link>
              <small>{l.note}</small>
            </div>
            <div className="sl-track" role="group" aria-label={l.label}
              onPointerMove={(e) => { interval?.move(e); point(e); }} onPointerDown={(e) => { interval?.down(e); point(e); }} onPointerLeave={() => shared?.(undefined)}
              onPointerUp={(e) => { if (selected) picker.current?.focus({ preventScroll: true }); interval?.up(e); }} onPointerCancel={() => interval?.cancel()} onLostPointerCapture={() => interval?.cancel()} onClickCapture={(e) => { if ((e.target as Element).closest(".timeline-mark")) shared?.(undefined); interval?.click(e); }} onDragStart={(e) => { if (interval) e.preventDefault(); }}>
              <span className="sl-base" />
              {l.marks}
              {selected && <span aria-hidden="true" className="chart-selected-interval" style={{ left: pct(frac(selected.start)), width: pct(frac(selected.end) - frac(selected.start)) }} />}
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
        <LaneLegend windows={tl.windows.length > 0} subjects={subjectLanes} gaps={subjectLanes && sysGaps.length + advGaps.length > 0} />
        <InfoTip label="Timeline lanes" text="Events are observed timestamps; coloured finding boxes show the worst check state in each time bucket. Dashed boxes show buckets with no observed check state. A box is not one incident or an exact onset. Shaded windows are inferred context, not proven causes. Hover or focus a mark to inspect it; activate it to open its evidence. Missing or truncated captures limit what the Timeline can show."/>
        <span className="right">
          {checks && (
            <button type="button" className="chip" aria-pressed={asTable} onClick={() => setAsTable((t) => !t)}>
              {asTable ? "Show chart" : "Show as table"}
            </button>
          )}
          {interval && <button ref={picker} type="button" className="chip" onClick={interval.picker}>Select interval</button>}
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

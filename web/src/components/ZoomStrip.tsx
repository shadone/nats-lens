// The zoom strip under the timeline: minute resolution over the newest
// window. Wide: Kubernetes above the axis, NATS and findings below. Narrow,
// or when the wide layout cannot place every label: time runs down.
import { useRef,type ReactNode } from "react";
import type { Timeline, Window } from "../api/model";
import {TimelineMark, EventDetails, timelineTime} from "./TimelineMark";
import {CapturedEventList} from "./TimelineEventEvidence";
import {evidenceRange} from "../lib/timelineEvidence";
import { hm } from "../lib/format";
import { useWidth } from "../lib/useWidth";
import { zoomItems } from "../lib/timelineModel";
import { TIER, V_FONT, V_OFFSET, layoutHorizontal, layoutVertical, zoomDomain, type HLayout, type ZoomItem } from "../lib/zoomLayout";

function Marker({ it, x, y, r = 3.2 }: { it: ZoomItem; x: number; y: number; r?: number }) {
  if (it.kind === "k8s") return <circle cx={x} cy={y} r={r} style={{ fill: it.col }} />;
  if (it.kind === "nats") return <rect x={x - 3} y={y - 3} width="6" height="6" style={{ fill: it.col }} />;
  return <path d={`M${x} ${y - 4.2}l4.2 4.2-4.2 4.2-4.2-4.2z`} style={{ fill: it.col }} />;
}

function Label({ it, x, y, anchor, text, size }: { it: ZoomItem; x: number; y: number; anchor: "start" | "end"; text: string; size: number }) {
  return (
    <text x={x.toFixed(1)} y={y} fontSize={size} textAnchor={anchor} style={{ fill: it.tcol }}>

      <tspan style={{ fill: "var(--text-muted)" }}>{`${hm(it.t)} `}</tspan>
      {text}
    </text>
  );
}

function Horizontal({ L, W, domain, win,details }: { L: HLayout; W: number; domain: [number, number]; win: Window;details:(it:ZoomItem)=>ReactNode }) {
  const { axisY, height: H, xs } = L;
  const step = (L.x1 - L.x0) / ((domain[1] - domain[0]) / 300_000) < 46 ? 600_000 : 300_000;
  const ticks: number[] = [];
  for (let t = domain[0]; t <= domain[1]; t += step) ticks.push(t);
  return (
    <svg className="zsvg" role="group" aria-labelledby="ztitle" viewBox={`0 0 ${W} ${H}`} height={H} data-forced={L.forced}>
      <rect x={xs(win.start).toFixed(1)} y="0" width={(xs(win.end) - xs(win.start)).toFixed(1)} height={H} style={{ fill: "var(--upgrade-fill)" }} />
      <text x="0" y="16" fontSize="11" style={{ fill: "var(--k8s)", fontFamily: "var(--sans)", fontWeight: 500 }}>
        Kubernetes
      </text>
      <text x="0" y={axisY + 30} fontSize="11" style={{ fill: "var(--text-2)", fontFamily: "var(--sans)", fontWeight: 500 }}>
        NATS, findings
      </text>
      <line x1={L.x0} x2={L.x1} y1={axisY} y2={axisY} style={{ stroke: "var(--border)" }} />
      {([[L.up, -1], [L.down, 1]] as const).flatMap(([list, side]) =>
        list.map((p) => {
          const y = side < 0 ? axisY - 14 - p.tier * TIER : axisY + 18 + p.tier * TIER;
          const my = y - 4;
          const x = +p.x.toFixed(1);
          return (
            <TimelineMark svg key={p.item.id} label={p.item.title} details={details(p.item)} preview={<><strong>{p.item.title}</strong><div>{timelineTime(p.item.t)}</div><div>{p.item.kind === "lens" ? "Finding transition" : p.item.kind === "k8s" ? "Kubernetes event" : "NATS event"}</div></>}>
              <line x1={x} x2={x} y1={side < 0 ? my : axisY + 4} y2={side < 0 ? axisY - 4 : my} style={{ stroke: p.item.col, strokeOpacity: 0.5 }} />
              <Marker it={p.item} x={x} y={my} />
              <Label it={p.item} x={p.tx} y={y} anchor={p.anchor} text={p.text} size={11} />
            </TimelineMark>
          );
        }),
      )}
      {ticks.map((t) => (
        <text
          key={t}
          x={xs(t).toFixed(1)}
          y={axisY + 3.5}
          fontSize="11"
          textAnchor={t === domain[0] ? "start" : t === domain[1] ? "end" : "middle"}
          style={{ fill: "var(--text-muted)", stroke: "var(--surface)", strokeWidth: "6px", paintOrder: "stroke" }}
        >
          {hm(t)}
        </text>
      ))}
    </svg>
  );
}

function Vertical({ items, W, domain, win,details }: { items: ZoomItem[]; W: number; domain: [number, number]; win: Window;details:(it:ZoomItem)=>ReactNode }) {
  const L = layoutVertical(items, W, domain);
  const { cx, top, ys } = L;
  const ticks: number[] = [];
  for (let t = domain[0] + 300_000; t < domain[1]; t += 300_000) ticks.push(t);
  return (
    <svg className="zsvg" role="group" aria-labelledby="ztitle" viewBox={`0 0 ${W} ${L.height}`} height={L.height}>
      <text x={cx - V_OFFSET} y="11" fontSize="11" textAnchor="end" style={{ fill: "var(--k8s)", fontFamily: "var(--sans)", fontWeight: 500 }}>
        Kubernetes
      </text>
      <text x={cx + V_OFFSET} y="11" fontSize="11" style={{ fill: "var(--text-2)", fontFamily: "var(--sans)", fontWeight: 500 }}>
        NATS, findings
      </text>
      <rect x={cx - 4} y={ys(win.start)} width="8" height={ys(win.end) - ys(win.start)} style={{ fill: "var(--upgrade-line)", fillOpacity: 0.35 }} />
      <line x1={cx} x2={cx} y1={top - 6} y2={ys(domain[1]) + 2} style={{ stroke: "var(--border)" }} />
      {ticks.map((t) => (
        <line key={t} x1={cx - 2} x2={cx + 2} y1={ys(t)} y2={ys(t)} style={{ stroke: "var(--text-faint)" }} />
      ))}
      {L.labels.map(({ item: it, y0, y, text }) => {
        const side = it.side;
        const xa = cx + side * 5;
        const xb = cx + side * (V_OFFSET - 4);
        return (
          <TimelineMark svg key={it.id} label={it.title} details={details(it)} preview={<><strong>{it.title}</strong><div>{timelineTime(it.t)}</div><div>{it.kind === "lens" ? "Finding transition" : it.kind === "k8s" ? "Kubernetes event" : "NATS event"}</div></>}>
            <polyline points={`${cx},${y0} ${xa},${y0} ${xb},${y - 3.5}`} style={{ fill: "none", stroke: it.col, strokeOpacity: 0.6 }} />
            <Marker it={it} x={cx} y={y0} r={2.6} />
            <Label it={it} x={cx + side * V_OFFSET} y={y} anchor={side < 0 ? "end" : "start"} text={text} size={V_FONT} />
          </TimelineMark>
        );
      })}
      <text x={cx} y={top - 9} fontSize="11" textAnchor="middle" style={{ fill: "var(--text-muted)", stroke: "var(--surface)", strokeWidth: "5px", paintOrder: "stroke" }}>
        {hm(domain[0])}
      </text>
      <text x={cx} y={ys(domain[1]) + 15} fontSize="11" textAnchor="middle" style={{ fill: "var(--text-muted)" }}>
        {hm(domain[1])}
      </text>
    </svg>
  );
}

export function ZoomStrip({ tl, win }: { tl: Timeline; win: Window }) {
  const box = useRef<HTMLDivElement>(null);
  const W = useWidth(box);
  const domain = zoomDomain(win.start, win.end);
  const source = zoomItems(tl, domain),items:ZoomItem[]=[];
  for(const it of source){const prior=items.find(x=>x.t===it.t&&x.side===it.side);if(prior){prior.events=[...(prior.events??[]),...(it.events??[])];prior.title=`${prior.events.length} overlapping observations`;prior.labels=[prior.title]}else items.push({...it})}
  const fixed=tl.evidenceRange??evidenceRange(tl.start,tl.start+tl.bucketCount*tl.bucketMs);
  const details=(it:ZoomItem)=>it.events?.length===1?<EventDetails event={it.events[0]} range={fixed} readId={tl.eventReadId}/>:<CapturedEventList ids={[...new Set((it.events??[]).flatMap(e=>e.eventId?[e.eventId]:[]))]} captured={it.events} range={fixed} readId={tl.eventReadId}/>;
  // The wide layout is used only when it places every label by the rules.
  const L = W >= 600 ? layoutHorizontal(items, W, domain) : undefined;
  const wide = L !== undefined && L.forced === 0;
  return (
    <div className="zoom" ref={box}>
      <div className="zoom-h">
        <span className="lbl" id="ztitle">
          {win.label}, {hm(domain[0])}–{hm(domain[1])} UTC
        </span>
        <span className="aside">
          {wide
            ? "Minute resolution: Kubernetes events above the axis, NATS advisories and finding transitions below."
            : "Minute resolution, time runs down. Labels shift down when events are close; the stem marks the exact minute."}
        </span>
        <span className="zkey" aria-hidden="true">
          <span>
            <svg viewBox="0 0 9 9">
              <circle cx="4.5" cy="4.5" r="3.5" style={{ fill: "var(--k8s)" }} />
            </svg>
            Kubernetes
          </span>
          <span>
            <svg viewBox="0 0 9 9">
              <rect x="1" y="1" width="7" height="7" style={{ fill: "var(--text-2)" }} />
            </svg>
            NATS advisory
          </span>
          <span>
            <svg viewBox="0 0 9 9">
              <path d="M4.5 .5l4 4-4 4-4-4z" style={{ fill: "var(--sev-warning)" }} />
            </svg>
            Finding transition
          </span>
        </span>
      </div>
      {W > 0 && (wide ? <Horizontal details={details} L={L} W={W} domain={domain} win={win} /> : <Vertical details={details} items={items} W={W} domain={domain} win={win} />)}
      <ul className="sr-only">
        {items.map((it) => (
          <li key={it.id}>
            {hm(it.t)} {it.title}
          </li>
        ))}
      </ul>
    </div>
  );
}

// The Topology page's map: a box per cluster with its servers, the gateway
// lines between clusters and a box per hub cluster for its leaf nodes. Up to
// four clusters sit in a row as on the design board; more, or a narrow
// card, give a grid of boxes and a table of the gateways.
import type { CSSProperties, MouseEvent } from "react";
import type { Topology, TopologyCluster, TopologyGateway } from "../api/topology";
import { fmtBytes, fmtShortDuration } from "../lib/format";
import {
  boxCentre,
  clusterLabel,
  clusterNote,
  clusterWarns,
  droppingServers,
  farBand,
  farLabels,
  gatewayLabelParts,
  gatewayText,
  gatewayWarns,
  labelTokens,
  leafBox,
  neighbours,
  quietLabel,
  routeLineParts,
  rowFractions,
  rowLayout,
  serverMemory,
} from "../lib/topologyView";
import { findingPath, serverPath } from "../lib/view";
import { Num, RLink } from "./common";

interface Props {
  t: Topology;
  /** The card's inner width; 0 when not yet measured. */
  width: number;
  /** The cluster the routes table shows. */
  picked?: string;
  onPick: (cluster: string) => void;
}

// The width the row is drawn for before the map is measured, in pixels.
const UNMEASURED_WIDTH = 1100;
// The band under the boxes when it holds only leaf node connectors, in pixels.
const BAND_LEAF_ONLY = 30;

export function TopologyMap({ t, width, picked, onPick }: Props) {
  const dropping = droppingServers(t.gateways);
  const pickable = t.clusters.length > 1;
  if (!rowLayout(t.clusters.length, width)) {
    return (
      <>
        <div className="tm-grid">
          {t.clusters.map((c) => (
            <ClusterBox key={c.name} c={c} t={t} dropping={dropping} picked={pickable && picked === c.name} pickable={pickable} onPick={onPick} withLeaf />
          ))}
        </div>
        {t.gateways.length > 0 && <GatewayTable gateways={t.gateways} />}
      </>
    );
  }

  const n = t.clusters.length;
  const { box, gap, slots } = rowFractions(n);
  const total = slots * box + (slots - 1) * gap;
  const used = n * box + (n - 1) * gap;
  const columns = [...t.clusters.flatMap((_, i) => (i === 0 ? [`minmax(0,${box}fr)`] : [`minmax(0,${gap}fr)`, `minmax(0,${box}fr)`]))];
  if (total > used) columns.push(`minmax(0,${total - used}fr)`);
  const gridTemplateColumns = columns.join(" ");
  const order = t.clusters.map((c) => c.name);
  const near = neighbours(order, t.gateways);
  const leaves = t.clusters.map(leafBox);
  const anyLeaf = leaves.some((l) => l !== undefined);
  // The brackets and labels are laid out in pixels of the map's width.
  const w = width || UNMEASURED_WIDTH;
  const far = farLabels(
    order,
    t.gateways,
    leaves.map((l) => l !== undefined),
    w,
  );
  const band = far.length > 0 ? farBand(far) : anyLeaf ? BAND_LEAF_ONLY : 0;
  const pct = (x: number) => `${(x / w) * 100}%`;

  return (
    <div className="tm">
      <div className="tm-row" style={{ gridTemplateColumns }}>
        {t.clusters.map((c, i) => (
          <ClusterRowCell key={c.name} c={c} i={i} t={t} gw={near.get(i - 1)} dropping={dropping} picked={pickable && picked === c.name} pickable={pickable} onPick={onPick} />
        ))}
      </div>
      {band > 0 && (
        <div className="tm-band" style={{ height: band }}>
          <svg width="100%" height={band} viewBox={`0 0 ${w} ${band}`} preserveAspectRatio="none" aria-hidden="true">
            {t.clusters.map((c, i) => {
              const x = boxCentre(i, n, w);
              return leaves[i] ? <line key={c.name} className="tm-leaf-line" x1={x} y1={0} x2={x} y2={band} vectorEffect="non-scaling-stroke" /> : null;
            })}
            {far.map((p) => (
              <polyline
                key={`${p.b.g.a}|${p.b.g.b}`}
                className={`tm-gw-line${gatewayWarns(p.b.g) ? " warn" : ""}`}
                points={`${p.legX},0 ${p.legX},${p.y} ${p.legEndX},${p.y} ${p.legEndX},0`}
                fill="none"
                vectorEffect="non-scaling-stroke"
              />
            ))}
          </svg>
          {far.map((p) => (
            // Just right of the bracket's first leg, wrapped to end before the next line it would cover.
            <GatewayLabel key={`${p.b.g.a}|${p.b.g.b}`} g={p.b.g} lines={p.tokens} style={{ left: pct(p.left), top: p.y }} />
          ))}
        </div>
      )}
      {anyLeaf && (
        <div className="tm-row tm-leaves" style={{ gridTemplateColumns }}>
          {t.clusters.map((c, i) => (
            <LeafCell key={c.name} c={c} i={i} />
          ))}
        </div>
      )}
    </div>
  );
}

function ClusterRowCell(p: { c: TopologyCluster; i: number; t: Topology; gw?: TopologyGateway; dropping: Set<string>; picked: boolean; pickable: boolean; onPick: (c: string) => void }) {
  return (
    <>
      {p.i > 0 && (p.gw ? <NearGateway g={p.gw} /> : <div className="tm-gap" />)}
      <ClusterBox c={p.c} t={p.t} dropping={p.dropping} picked={p.picked} pickable={p.pickable} onPick={p.onPick} />
    </>
  );
}

/** A gateway between two boxes next to each other: a line across the gap with its label above. */
function NearGateway({ g }: { g: TopologyGateway }) {
  return (
    <div className={`tm-near${gatewayWarns(g) ? " warn" : ""}`}>
      <GatewayLabel g={g} />
      <span className="tm-near-line" aria-hidden="true" />
    </div>
  );
}

/**
 * A gateway's label. Each part stays on one line and every part after the
 * first starts with "· ", so a wrapped line starts with the separator.
 * With lines, the parts are broken into those lines; without, the browser
 * wraps them in the space it has.
 */
function GatewayLabel({ g, style, lines }: { g: TopologyGateway; style?: CSSProperties; lines?: string[][] }) {
  const text = gatewayText(g);
  const rows = lines ?? [labelTokens(gatewayLabelParts(g))];
  return (
    <span className={`tm-gw-label mono${gatewayWarns(g) ? " warn" : ""}${lines ? " far" : ""}`} style={style} title={text}>
      <span aria-hidden="true">
        {rows.map((row, r) => (
          <span key={r} className={lines ? "tm-gw-row" : undefined}>
            {row.map((tok, k) => (
              <span key={tok}>
                {k > 0 && " "}
                <span className="tm-nw">{tok}</span>
              </span>
            ))}
          </span>
        ))}
      </span>
      <span className="sr-only">{text}</span>
    </span>
  );
}

function LeafCell({ c, i }: { c: TopologyCluster; i: number }) {
  const l = leafBox(c);
  return (
    <>
      {i > 0 && <div />}
      <div className="tm-leaf-slot">{l && <LeafBox c={c} />}</div>
    </>
  );
}

function LeafBox({ c }: { c: TopologyCluster }) {
  const l = leafBox(c);
  if (!l) return null;
  return (
    <div className="tm-leaf">
      <div className="lbl">Leaf nodes on {clusterLabel(c.name)}</div>
      <div className="tm-leaf-n mono">
        {l.sites}{" "}
        <span>
          <span className="tm-nw">{l.unit}</span> <span className="tm-nw">· {l.state}</span>
        </span>
      </div>
    </div>
  );
}

function ClusterBox(p: { c: TopologyCluster; t: Topology; dropping: Set<string>; picked: boolean; pickable: boolean; onPick: (c: string) => void; withLeaf?: boolean }) {
  const { c } = p;
  const note = clusterNote(c, p.t.gateways, p.t.routes, p.t.replication);
  const routeLine = routeLineParts(c, p.t.routes);
  // A click anywhere on the box but a server's link picks the cluster; the name is the keyboard's way.
  const onClick = (e: MouseEvent) => {
    if (!p.pickable || (e.target as HTMLElement).closest("a,button")) return;
    p.onPick(c.name);
  };
  return (
    <div className={`tm-box${clusterWarns(c, p.dropping, p.t.gateways, p.t.routes) ? " warn" : ""}${p.picked ? " on" : ""}${p.pickable ? " pickable" : ""}`} onClick={onClick}>
      <div className="tm-box-h">
        {p.pickable ? (
          <button type="button" className="tm-name mono" aria-pressed={p.picked} onClick={() => p.onPick(c.name)} title={`Show the routes inside ${clusterLabel(c.name)}`}>
            {clusterLabel(c.name)}
            <span className="sr-only">, show its routes</span>
          </button>
        ) : (
          <span className="tm-name mono">{clusterLabel(c.name)}</span>
        )}
        {c.place && <span className="tm-place">{c.place}</span>}
      </div>
      <ul className="tm-chips" aria-label={`Servers of ${clusterLabel(c.name)}`}>
        {c.servers.map((s) => {
          const mem = serverMemory(s);
          const why = s.missing ? "missing" : p.dropping.has(s.name) ? "gateway dropping" : (mem.warn ?? "");
          const facts = [mem.text && `memory ${mem.text}`, mem.js?.text].filter(Boolean).join("; ");
          const title = [s.name, why, facts].filter(Boolean).join(": ");
          return (
            <li key={s.name}>
              <RLink to={serverPath(s.name)} className={`tm-chip mono${why ? " warn" : ""}`} title={title}>
                {s.name}
                {mem.text && (
                  <span className="tm-mem" aria-hidden="true">
                    {mem.text}
                  </span>
                )}
                {mem.js?.short && (
                  <span className="tm-mem" aria-hidden="true">
                    {mem.js.short}
                  </span>
                )}
                {facts && <span className="sr-only">{`, ${facts}`}</span>}
                {why && <span className="sr-only">{`, ${why}`}</span>}
              </RLink>
              {s.memoryFindingId && (
                <RLink to={findingPath(s.memoryFindingId)} className={`tm-finding mono${s.memoryFindingQuiet ? "" : " tp-warn"}`}>
                  {s.memoryFindingCode}
                  {s.memoryFindingQuiet && <span className="sr-only">{quietLabel(true, s.memoryFindingAccepted)}</span>}
                </RLink>
              )}
            </li>
          );
        })}
      </ul>
      {routeLine.parts.length > 0 && (
        <div className="tm-routes mono">
          {routeLine.parts.map((part, k) => (
            <span key={part} className={`tm-nw${k > 0 ? " tp-warn" : ""}`}>
              {k > 0 && " · "}
              {part}
            </span>
          ))}
        </div>
      )}
      {note && <div className="tm-note">{note}</div>}
      {p.withLeaf && <LeafBox c={c} />}
    </div>
  );
}

/** The gateways as a table: the map's text at a narrow width or with many clusters. */
function GatewayTable({ gateways }: { gateways: TopologyGateway[] }) {
  return (
    <div className="table-wrap scroll-cue tm-gw-table">
      <table className="tbl">
        <caption className="sr-only">Gateways between clusters</caption>
        <thead>
          <tr>
            <th scope="col">Gateway</th>
            <th scope="col">RTT</th>
            <th scope="col">Up</th>
            <th scope="col">Drops 1 h</th>
            <th scope="col">Drops 24 h</th>
            <th scope="col">Pending</th>
          </tr>
        </thead>
        <tbody>
          {gateways.map((g) => (
            <tr key={`${g.a}|${g.b}`} className={gatewayWarns(g) ? "warn" : undefined}>
              <th scope="row" className="mono">
                {clusterLabel(g.a)} – {clusterLabel(g.b)}
              </th>
              <td className="mono">
                <Num v={g.rttMedianMs} fmt={fmtShortDuration} />
              </td>
              <td className="mono">
                {g.connections} of {g.expected}
              </td>
              <td className="mono">
                <Num v={g.drops1h} />
              </td>
              <td className="mono">
                <Num v={g.drops24h} />
              </td>
              <td className="mono">
                <Num v={g.pendingBytes} fmt={fmtBytes} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

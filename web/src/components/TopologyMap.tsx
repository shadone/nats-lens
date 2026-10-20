// The Topology page's map: a box per cluster with its servers, the gateway
// lines between clusters and a box per hub cluster for its leaf nodes. Up to
// four clusters sit in a row as on the design board; more, or a narrow
// card, give a grid of boxes and a table of the gateways.
import type { CSSProperties, MouseEvent } from "react";
import type { Topology, TopologyCluster, TopologyGateway } from "../api/topology";
import { fmtBytes, fmtShortDuration } from "../lib/format";
import {
  brackets,
  clusterNote,
  clusterWarns,
  droppingServers,
  gatewayLabel,
  gatewayText,
  gatewayWarns,
  leafBox,
  neighbours,
  rowLayout,
  type Bracket,
} from "../lib/topologyView";
import { serverPath } from "../lib/view";
import { Num, RLink } from "./common";

interface Props {
  t: Topology;
  /** The card's inner width; 0 when not yet measured. */
  width: number;
  /** The cluster the routes table shows. */
  picked?: string;
  onPick: (cluster: string) => void;
}

// Columns of the row, in fractions: a box, then the gap a gateway label sits in.
const BOX = 5;
const GAP = 3;
// The row is laid out for at least this many clusters, so one or two boxes keep the board's width.
const MIN_SLOTS = 3;
// SVG units per fraction.
const U = 100;
// A leg's distance from its box's centre per step, in fractions.
const LEG_STEP = 0.12 * BOX;
// Heights of the band under the boxes, in pixels.
const BAND_TOP = 22;
const BAND_LEVEL = 30;
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
  const slots = Math.max(n, MIN_SLOTS);
  const total = slots * BOX + (slots - 1) * GAP;
  const used = n * BOX + (n - 1) * GAP;
  const columns = [...t.clusters.flatMap((_, i) => (i === 0 ? [`minmax(0,${BOX}fr)`] : [`minmax(0,${GAP}fr)`, `minmax(0,${BOX}fr)`]))];
  if (total > used) columns.push(`minmax(0,${total - used}fr)`);
  const gridTemplateColumns = columns.join(" ");
  const order = t.clusters.map((c) => c.name);
  const near = neighbours(order, t.gateways);
  const far = brackets(order, t.gateways);
  const leaves = t.clusters.map(leafBox);
  const anyLeaf = leaves.some((l) => l !== undefined);
  const levels = far.reduce((m, b) => Math.max(m, b.level + 1), 0);
  const band = levels > 0 ? BAND_TOP + levels * BAND_LEVEL : anyLeaf ? BAND_LEAF_ONLY : 0;
  const centre = (i: number) => (i * (BOX + GAP) + BOX / 2) * U;
  const pct = (x: number) => `${(x / (total * U)) * 100}%`;
  const levelY = (l: number) => BAND_TOP + l * BAND_LEVEL;
  const legX = (b: Bracket, end: "i" | "j") => (end === "i" ? centre(b.i) + (b.legI + 1) * LEG_STEP * U : centre(b.j) - (b.legJ + 1) * LEG_STEP * U);

  return (
    <div className="tm">
      <div className="tm-row" style={{ gridTemplateColumns }}>
        {t.clusters.map((c, i) => (
          <ClusterRowCell key={c.name} c={c} i={i} t={t} gw={near.get(i - 1)} dropping={dropping} picked={pickable && picked === c.name} pickable={pickable} onPick={onPick} />
        ))}
      </div>
      {band > 0 && (
        <div className="tm-band" style={{ height: band }}>
          <svg width="100%" height={band} viewBox={`0 0 ${total * U} ${band}`} preserveAspectRatio="none" aria-hidden="true">
            {t.clusters.map((c, i) =>
              leaves[i] ? <line key={c.name} className="tm-leaf-line" x1={centre(i)} y1={0} x2={centre(i)} y2={band} vectorEffect="non-scaling-stroke" /> : null,
            )}
            {far.map((b) => {
              const y = levelY(b.level);
              const x1 = legX(b, "i");
              const x2 = legX(b, "j");
              return (
                <polyline
                  key={`${b.g.a}|${b.g.b}`}
                  className={`tm-gw-line${gatewayWarns(b.g) ? " warn" : ""}`}
                  points={`${x1},0 ${x1},${y} ${x2},${y} ${x2},0`}
                  fill="none"
                  vectorEffect="non-scaling-stroke"
                />
              );
            })}
          </svg>
          {far.map((b) => (
            <GatewayLabel key={`${b.g.a}|${b.g.b}`} g={b.g} style={{ left: pct((legX(b, "i") + legX(b, "j")) / 2), top: levelY(b.level) }} />
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

function GatewayLabel({ g, style }: { g: TopologyGateway; style?: CSSProperties }) {
  const text = gatewayText(g);
  return (
    <span className={`tm-gw-label mono${gatewayWarns(g) ? " warn" : ""}`} style={style} title={text}>
      <span aria-hidden="true">{gatewayLabel(g)}</span>
      <span className="sr-only">{text}</span>
    </span>
  );
}

function LeafCell({ c, i }: { c: TopologyCluster; i: number }) {
  const l = leafBox(c);
  return (
    <>
      {i > 0 && <div />}
      <div className="tm-leaf-slot">{l && <LeafBox c={c} sites={l.sites} rest={l.rest} />}</div>
    </>
  );
}

function LeafBox({ c, sites, rest }: { c: TopologyCluster; sites: number; rest: string }) {
  return (
    <div className="tm-leaf">
      <div className="lbl">Leaf nodes on {c.name || "the cluster"}</div>
      <div className="tm-leaf-n mono">
        {sites} <span>{rest}</span>
      </div>
    </div>
  );
}

function ClusterBox(p: { c: TopologyCluster; t: Topology; dropping: Set<string>; picked: boolean; pickable: boolean; onPick: (c: string) => void; withLeaf?: boolean }) {
  const { c } = p;
  const note = clusterNote(c, p.t.gateways);
  const leaf = p.withLeaf ? leafBox(c) : undefined;
  // A click anywhere on the box but a server's link picks the cluster; the name is the keyboard's way.
  const onClick = (e: MouseEvent) => {
    if (!p.pickable || (e.target as HTMLElement).closest("a,button")) return;
    p.onPick(c.name);
  };
  return (
    <div className={`tm-box${clusterWarns(c, p.dropping) ? " warn" : ""}${p.picked ? " on" : ""}${p.pickable ? " pickable" : ""}`} onClick={onClick}>
      <div className="tm-box-h">
        {p.pickable ? (
          <button type="button" className="tm-name mono" aria-pressed={p.picked} onClick={() => p.onPick(c.name)} title={`Show the routes inside ${c.name}`}>
            {c.name || "(no name)"}
            <span className="sr-only">, show its routes</span>
          </button>
        ) : (
          <span className="tm-name mono">{c.name || "(no name)"}</span>
        )}
        {c.place && <span className="tm-place">{c.place}</span>}
      </div>
      <ul className="tm-chips" aria-label={`Servers of ${c.name || "the cluster"}`}>
        {c.servers.map((s) => {
          const why = s.missing ? "missing" : p.dropping.has(s.name) ? "gateway dropping" : "";
          return (
            <li key={s.name}>
              <RLink to={serverPath(s.name)} className={`tm-chip mono${why ? " warn" : ""}`} title={why ? `${s.name}: ${why}` : s.name}>
                {s.name}
                {why && <span className="sr-only">{`, ${why}`}</span>}
              </RLink>
            </li>
          );
        })}
      </ul>
      {note && <div className="tm-note">{note}</div>}
      {leaf && <LeafBox c={c} sites={leaf.sites} rest={leaf.rest} />}
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
                {g.a} – {g.b}
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

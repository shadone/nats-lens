// Topology: how the servers of a system connect in the range's newest
// collection. A map of the clusters, their gateways and leaf node hubs,
// then the leaf node sites worst first and the routes inside one cluster.
// The cluster of the routes table lives in the URL as ?cluster=.
import { useSearchParams } from "react-router";
import { useTopology } from "../api/queries";
import type { Topology as TopologyData } from "../api/topology";
import { Empty, QueryView } from "../components/common";
import { TopologyMap } from "../components/TopologyMap";
import { LeafTable, RouteTable } from "../components/TopologyTables";
import { dayMonHm, hms } from "../lib/format";
import { dropsLine, leafBox, routesCluster, summary, unobservedLines } from "../lib/topologyView";
import { usePageTitle } from "../lib/usePageTitle";
import { useTimeRange } from "../lib/useTimeRange";
import { useElementWidth } from "../lib/useWidth";

const CLUSTER_PARAM = "cluster";

/** The servers whose LEAFZ did not answer, by their cluster. */
function leafzFailedByCluster(t: TopologyData): Record<string, string[]> {
  const of = new Map(t.clusters.flatMap((c) => c.servers.map((s) => [s.name, c.name] as const)));
  const out: Record<string, string[]> = {};
  for (const u of t.unobserved) {
    if (u.endpoint !== "LEAFZ") continue;
    for (const s of u.servers) (out[of.get(s) ?? ""] ??= []).push(s);
  }
  return out;
}
const DAY = 86_400_000;

export function Topology() {
  usePageTitle("Topology");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const q = useTopology(spec);
  const t = q.data;
  const line = t?.collected ? summary(t) : "";

  const pick = (c: string) =>
    setParams(
      (p) => {
        const n = new URLSearchParams(p);
        n.set(CLUSTER_PARAM, c);
        return n;
      },
      { replace: true },
    );

  return (
    <>
      <div className="page-h">
        <span className="aside">
          {line ? `${line}, ` : "How the servers connect, "}
          {t?.epochAt !== undefined ? (
            <>
              in the collection of <span className="mono">{hms(t.epochAt)} UTC</span>
            </>
          ) : (
            "in the newest collection of the range"
          )}
          .
        </span>
      </div>
      <QueryView q={q} what="the topology">
        {(d) => (d.collected ? <Body t={d} asked={params.get(CLUSTER_PARAM)} onPick={pick} /> : <Empty>No collection in this range.</Empty>)}
      </QueryView>
    </>
  );
}

function Body({ t, asked, onPick }: { t: TopologyData; asked: string | null; onPick: (c: string) => void }) {
  const [mapRef, width] = useElementWidth();
  const picked = routesCluster(t, asked);
  const anyLeaf = t.leafnodes.length > 0 || t.clusters.some((c) => leafBox(c) !== undefined);
  const showMap = t.clusters.length > 1 || anyLeaf;
  const drops = t.gateways.length > 0 || anyLeaf;
  const notes = unobservedLines(t);
  const shortHistory = t.dropsWindowStart !== undefined && t.epochAt !== undefined && t.epochAt - t.dropsWindowStart < DAY - 60_000;
  return (
    <>
      {showMap && (
        <section className="card tp-map" aria-label="Map of clusters, gateways and leaf nodes">
          <div ref={mapRef}>
            <TopologyMap t={t} width={width} picked={picked} onPick={onPick} />
          </div>
        </section>
      )}
      {(drops || notes.length > 0) && (
        <div className="tp-notes">
          {drops && (
            <p>
              {dropsLine(t)}
              {shortHistory && ` The history starts at ${dayMonHm(t.dropsWindowStart!)} UTC, so fewer than 24 hours are counted.`}
            </p>
          )}
          {notes.map((n) => (
            <p key={n} className="tp-unobserved">
              {n}
            </p>
          ))}
        </div>
      )}
      <div className={`tp-tables${t.leafnodes.length > 0 ? " two" : ""}`}>
        {t.leafnodes.length > 0 && <LeafTable rows={t.leafnodes} total={t.leafnodesTotal} at={t.epochAt} leafzFailed={leafzFailedByCluster(t)} />}
        <RouteTable routes={t.routes} clusters={t.clusters} picked={picked} onPick={onPick} single={t.clusters.length === 1 && !showMap} />
      </div>
    </>
  );
}

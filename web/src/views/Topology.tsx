import type { TopologyModel } from "../pages/Topology";
// Topology: how the servers of a system connect in the range's newest
// collection. A map of the clusters, their gateways and leaf node hubs,
// then the leaf node sites worst first and the routes inside one cluster.
// The cluster of the routes table lives in the URL as ?cluster=.
import type { Topology as TopologyData } from "../api/topology";
import { Empty, QueryView, RLink } from "../components/common";
import { TopologyMap } from "../components/TopologyMap";
import { LeafTable, ReplicationTable, RouteTable } from "../components/TopologyTables";
import { dayMonHm, hms } from "../lib/format";
import { dropsLine, leafBox, routesCluster, unobservedLines } from "../lib/topologyView";
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

export function TopologyView({ model }: { model: TopologyModel }) {
  const { params, q, t, line, pick } = model;

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
  const drops = t.gateways.length > 0 || anyLeaf || t.routes.some((r) => r.drops24h !== undefined);
  const notes = unobservedLines(t);
  const anyMem = t.clusters.some((c) => c.servers.some((s) => s.memBytes !== undefined));
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
      {(drops || notes.length > 0 || (showMap && anyMem)) && (
        <div className="tp-notes">
          {drops && (
            <p>
              {dropsLine(t)}
              {shortHistory && ` The history starts at ${dayMonHm(t.dropsWindowStart!)} UTC, so fewer than 24 hours are counted.`}
            </p>
          )}
          {showMap && anyMem && (
            <p>
              Under each server is its process memory. JetStream memory stores are on <RLink to="/capacity">Capacity</RLink>.
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
      {t.replication.length > 0 && (
        <div className="tp-tables">
          <ReplicationTable rows={t.replication} />
        </div>
      )}
    </>
  );
}

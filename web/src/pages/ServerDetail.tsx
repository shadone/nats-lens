// One server: five metric boxes, where it runs, routes, limits and
// settings, recent events, the endpoints that answered, findings and
// process lifetimes.
import { useParams } from "react-router";
import type { Entity } from "../api/model";
import { useServerInfo, type ServerRow } from "../api/servers";
import { Empty, QueryView, RLink } from "../components/common";
import { EndpointsAnswered, LimitsCard, RecentEvents, Routes, WhereItRuns } from "../components/ServerDetailParts";
import { EntityFindings } from "../components/EntityParts";
import { HealthIcon } from "../components/Severity";
import { ServerTiles } from "../components/ServerTiles";
import { dayMonHm, fmtDuration, isoMinute } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";

function statusLine(s: ServerRow): string {
  return [
    s.role !== "unknown" ? s.role : "",
    s.version ? `v${s.version}` : "",
    s.present && s.uptimeMs !== undefined ? `up ${fmtDuration(s.uptimeMs)}${s.startTime !== undefined ? ` since ${dayMonHm(s.startTime)} UTC` : ""}` : "",
    s.present ? "" : "did not answer in the newest epoch",
  ]
    .filter(Boolean)
    .join(" · ");
}

export function ServerDetail() {
  const name = useParams().name ?? "";
  usePageTitle(name);
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const q = useServerInfo(spec, name);
  const entity: Entity = { kind: "server", key: name, name, account: "", stream: "", consumer: "", server: name };
  const cluster = q.data?.server?.cluster;
  return (
    <>
      <nav className="crumbs" aria-label="Breadcrumb">
        <RLink to="/servers">Servers</RLink>
        {cluster && <span>/ {cluster}</span>}
      </nav>
      <QueryView q={q} what="the server">
        {(d) => {
          const s = d.server;
          if (!s) return <Empty>The server {name} was not seen in this range.</Empty>;
          return (
            <>
              <div className="page-h">
                <HealthIcon level={s.status} />
                <h1 className="mono">{name}</h1>
                <span className="aside">{statusLine(s)}</span>
              </div>
              <ServerTiles info={d} spec={spec} start={start} end={end} />
              <div className="sd-cards">
                <WhereItRuns info={d} />
                <Routes info={d} />
                <LimitsCard info={d} />
                <RecentEvents name={name} spec={spec} end={end} />
              </div>
              <EndpointsAnswered info={d} />
              <EntityFindings entity={entity} />
              <section className="sec">
                <div className="sec-h">
                  <h2>Processes</h2>
                  <span className="aside">Each process lifetime in the range; a restart starts a new server ID.</span>
                </div>
                <div className="card table-wrap">
                  <table className="tbl">
                    <thead>
                      <tr>
                        <th scope="col">Server ID</th>
                        <th scope="col">Started, UTC</th>
                        <th scope="col">First answered</th>
                        <th scope="col">Last answered</th>
                      </tr>
                    </thead>
                    <tbody>
                      {d.processes.map((p) => (
                        <tr key={p.serverId}>
                          <td className="mono">{p.serverId}</td>
                          <td className="mono">{p.startTime !== undefined ? isoMinute(p.startTime) : "–"}</td>
                          <td className="mono">{p.firstSeen !== undefined ? isoMinute(p.firstSeen) : "–"}</td>
                          <td className="mono">{p.lastSeen !== undefined ? isoMinute(p.lastSeen) : "–"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </section>
            </>
          );
        }}
      </QueryView>
    </>
  );
}

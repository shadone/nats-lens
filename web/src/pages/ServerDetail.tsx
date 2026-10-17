// One server: numbers, series over the range, monitoring endpoint outcomes,
// process lifetimes, findings and events.
import { useParams } from "react-router";
import type { Entity } from "../api/model";
import { useOverview, useSeries, useServer } from "../api/queries";
import { QuerySeriesChart } from "../charts/SeriesChart";
import { Dash, Empty, QueryView, RLink, Section, Num } from "../components/common";
import { EntityEvents, EntityFindings, Stat } from "../components/EntityParts";
import { HealthIcon } from "../components/Severity";
import { fmtBytes, fmtCount, fmtDuration, isoMinute } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { serverPath } from "../lib/view";

const SERIES: [string, string][] = [
  ["server.connections", "Connections"],
  ["server.in_msgs_rate", "Messages in"],
  ["server.out_msgs_rate", "Messages out"],
  ["server.in_bytes_rate", "Bytes in"],
  ["server.out_bytes_rate", "Bytes out"],
  ["server.mem_bytes", "Memory"],
  ["server.cpu_percent", "CPU"],
  ["server.js_storage_bytes", "JetStream storage"],
];

function Chart({ metric, title, entity, start, end }: { metric: string; title: string; entity: Entity; start: number; end: number }) {
  const { spec } = useTimeRange();
  const q = useSeries(spec, metric, entity);
  const windows = useOverview(spec).data?.windows ?? [];
  return <QuerySeriesChart q={q} title={title} start={start} end={end} windows={windows} />;
}

export function ServerDetail() {
  const name = useParams().name ?? "";
  usePageTitle(name);
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const q = useServer(spec, name);
  const entity: Entity = { kind: "server", key: name, name, account: "", stream: "", consumer: "", server: name };
  return (
    <>
      <nav className="crumbs" aria-label="Breadcrumb">
        <RLink to="/servers">Servers</RLink>
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
                <span className="aside">
                  v{s.version}, {s.role}, cluster {s.cluster || "–"}
                  {!s.present && ", did not answer in the newest epoch"}
                </span>
              </div>
              <div className="stats">
                <Stat label="Uptime" value={s.uptimeMs !== undefined ? fmtDuration(s.uptimeMs) : <Dash />} sub={s.startTime !== undefined ? `since ${isoMinute(s.startTime)} UTC` : undefined} />
                <Stat label="Healthz" value={s.healthz || <Dash />} bad={!!s.healthz && s.healthz !== "ok"} sub={s.healthzError || undefined} />
                <Stat label="Connections" value={<Num v={s.connections} />} sub={<RLink to={`/connections?server=${encodeURIComponent(name)}`}>list</RLink>} />
                <Stat label="Subscriptions" value={<Num v={d.subscriptions} />} />
                <Stat label="Slow consumers" value={<Num v={d.slowConsumers} />} sub="since the process started" />
                <Stat label="Routes" value={<Num v={d.routes} />} />
                <Stat label="JetStream" value={<><Num v={d.jsStreams} /> / <Num v={d.jsConsumers} /></>} sub="streams / consumers with a replica here" />
                <Stat label="Storage" value={<Num v={s.jsStorageBytes} fmt={fmtBytes} />} sub={s.jsMaxStorageBytes !== undefined ? <>of <Num v={s.jsMaxStorageBytes} fmt={fmtBytes} /></> : undefined} />
                <Stat label="Memory, CPU" value={<><Num v={s.memBytes} fmt={fmtBytes} />, <Num v={s.cpuPercent} fmt={fmtCount} />{s.cpuPercent !== undefined && "%"}</>} />
                <Stat label="Meta leader" value={d.metaLeader ? <RLink to={serverPath(d.metaLeader)}>{d.metaLeader}</RLink> : <Dash />} sub="in this server's view" />
                <Stat label="Config digest" value={<span className="mono">{d.configDigest || "–"}</span>} />
              </div>
              <section className="sec grid3">
                {SERIES.map(([m, t]) => (
                  <Chart key={m} metric={m} title={t} entity={entity} start={start} end={end} />
                ))}
              </section>
              <EntityFindings entity={entity} />
              <Section title="Monitoring endpoints" aside="Outcome of every request to the server in the newest epoch.">
                {d.endpoints.length ? (
                  <div className="card box-card ep">
                    {d.endpoints.map((e) => (
                      <span key={e.endpoint} className={e.state} title={e.error || e.state}>
                        {e.endpoint} {e.state}
                      </span>
                    ))}
                  </div>
                ) : (
                  <Empty>No requests recorded.</Empty>
                )}
              </Section>
              <Section title="Processes" aside="Each process lifetime in the range; a restart starts a new server ID.">
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
              </Section>
              <EntityEvents entity={entity} query={`server=${encodeURIComponent(name)}`} />
            </>
          );
        }}
      </QueryView>
    </>
  );
}

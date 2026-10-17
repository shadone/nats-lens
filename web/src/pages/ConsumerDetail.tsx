// One consumer: backlog numbers and series, findings, each server's view and
// where they disagree, configuration and events.
import { useParams } from "react-router";
import type { Entity } from "../api/model";
import { useConsumer, useOverview, useSeries } from "../api/queries";
import { QuerySeriesChart } from "../charts/SeriesChart";
import { Dash, Empty, KV, QueryView, RLink, Section, Num } from "../components/common";
import { Disagreements, EntityEvents, EntityFindings, Stat } from "../components/EntityParts";
import { HealthIcon } from "../components/Severity";
import { fmtDuration, isoMinute } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { serverPath, streamPath, withAccountName } from "../lib/view";

const SERIES: [string, string][] = [
  ["consumer.num_pending", "Pending"],
  ["consumer.num_ack_pending", "Ack pending"],
  ["consumer.num_redelivered", "Redelivered"],
];

function Chart({ metric, title, entity, start, end }: { metric: string; title: string; entity: Entity; start: number; end: number }) {
  const { spec } = useTimeRange();
  const q = useSeries(spec, metric, entity);
  const windows = useOverview(spec).data?.windows ?? [];
  return <QuerySeriesChart q={q} title={title} start={start} end={end} windows={windows} />;
}

export function ConsumerDetail() {
  const p = useParams();
  const account = p.account ?? "";
  const stream = p.stream ?? "";
  const name = p.consumer ?? "";
  usePageTitle(name);
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const q = useConsumer(spec, account, stream, name);
  const entity: Entity = withAccountName({ kind: "consumer", key: `${account}/${stream}/${name}`, name, account, stream, consumer: name, server: "" },
    [q.data?.consumer?.entity]);
  return (
    <>
      <nav className="crumbs" aria-label="Breadcrumb">
        <RLink to="/consumers">Consumers</RLink>
        <span aria-hidden="true">/</span>
        <RLink to={streamPath(account, stream)}>{stream}</RLink>
      </nav>
      <QueryView q={q} what="the consumer">
        {(d) => {
          const c = d.consumer;
          if (!c) return <Empty>The consumer {name} was not seen in this range.</Empty>;
          return (
            <>
              <div className="page-h">
                <HealthIcon level={c.status} />
                <h1 className="mono">{name}</h1>
                <span className="aside">
                  on <RLink to={streamPath(account, stream)}>{stream}</RLink>, {c.durable ? "durable" : "ephemeral"}, R{c.replicas ?? "–"}
                  {c.paused && ", paused"}
                  {c.serversDisagree && ", servers disagree"}
                </span>
              </div>
              {!c.present && (
                <div className="state-box err" role="status">
                  <b>Missing.</b> The consumer does not exist now: it is expected by the inventory, or it was deleted inside the range.
                </div>
              )}
              <div className="stats">
                <Stat label="Pending" value={<Num v={c.numPending} />} />
                <Stat label="Ack pending" value={<Num v={c.numAckPending} />} />
                <Stat label="Redelivered" value={<Num v={c.numRedelivered} />} />
                <Stat label="Waiting pulls" value={<Num v={c.numWaiting} />} />
                <Stat label="Leader" value={c.leader ? <RLink to={serverPath(c.leader)}>{c.leader}</RLink> : <Dash />} />
                <Stat label="Last delivery" value={c.lastDelivered !== undefined ? `${fmtDuration(end - c.lastDelivered)} ago` : <Dash />} sub={c.lastDelivered !== undefined ? `${isoMinute(c.lastDelivered)} UTC` : undefined} />
              </div>
              <section className="sec grid3">
                {SERIES.map(([m, t]) => (
                  <Chart key={m} metric={m} title={t} entity={entity} start={start} end={end} />
                ))}
              </section>
              <EntityFindings entity={entity} />
              <Disagreements items={d.disagreements} />
              <Section title="Per server" aside="What each server that holds the consumer reports.">
                {d.servers.length ? (
                  <div className="card table-wrap">
                    <table className="tbl">
                      <thead>
                        <tr>
                          <th scope="col">Server</th>
                          <th scope="col">Replica</th>
                          <th scope="col" className="num">
                            Lag
                          </th>
                          <th scope="col" className="num">
                            Delivered seq
                          </th>
                          <th scope="col" className="num">
                            Ack floor seq
                          </th>
                          <th scope="col">Created, UTC</th>
                          <th scope="col">Config hash</th>
                        </tr>
                      </thead>
                      <tbody>
                        {d.servers.map((v) => (
                          <tr key={v.server} className={v.isLeader ? "leader" : undefined}>
                            <td className="mono">
                              <RLink to={serverPath(v.server)}>{v.server}</RLink>
                            </td>
                            <td className={v.offline ? "bad" : v.current || v.isLeader ? "ok" : undefined}>{v.isLeader ? "leader" : v.offline ? "offline" : v.current ? "current" : "behind"}</td>
                            <td className="num"><Num v={v.lag} /></td>
                            <td className="num"><Num v={v.deliveredStreamSeq} /></td>
                            <td className="num"><Num v={v.ackFloorStreamSeq} /></td>
                            <td className="mono">{v.created !== undefined ? isoMinute(v.created) : <Dash />}</td>
                            <td className="mono">{v.configHash || <Dash />}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                ) : (
                  <Empty>No server reports this consumer now.</Empty>
                )}
              </Section>
              <Section title="Configuration" aside="From the leader's view.">
                <div className="card box-card">{d.config.length ? <KV items={d.config} /> : <p className="muted-note">Not reported.</p>}</div>
              </Section>
              <EntityEvents entity={entity} query={`consumer=${encodeURIComponent(entity.key)}`} />
            </>
          );
        }}
      </QueryView>
    </>
  );
}

// One stream: numbers, series over the range and 7 days, findings, each
// server's view and where they disagree, configuration, consumers, events.
import { useParams } from "react-router";
import type { Entity } from "../api/model";
import { useOverview, useSeries, useStream } from "../api/queries";
import { QuerySeriesChart, SeriesChart } from "../charts/SeriesChart";
import { Dash, Empty, KV, QueryView, RLink, Section, errorText, Num } from "../components/common";
import { ConsumersTable, Disagreements, EntityEvents, EntityFindings, Stat } from "../components/EntityParts";
import { HealthIcon } from "../components/Severity";
import { fmtBytes, fmtGrowth, isoMinute } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { AccountName } from "../components/AccountName";
import { consumerCount, serverPath, withAccountName } from "../lib/view";

export function StreamDetail() {
  const p = useParams();
  const account = p.account ?? "";
  const name = p.stream ?? "";
  usePageTitle(name);
  const { spec } = useTimeRange();
  const { start, end } = useResolvedRange(spec);
  const q = useStream(spec, account, name);
  const windows = useOverview(spec).data?.windows ?? [];
  const entity: Entity = withAccountName({ kind: "stream", key: `${account}/${name}`, name, account, stream: name, consumer: "", server: "" },
    [q.data?.stream?.entity]);
  const msgs = useSeries(spec, "stream.messages", entity);
  const bytes = useSeries(spec, "stream.bytes", entity);
  return (
    <>
      <nav className="crumbs" aria-label="Breadcrumb">
        <RLink to="/streams">Streams</RLink>
        <span aria-hidden="true">/</span>
        <AccountName id={account} name={q.data?.stream?.entity.accountName} />
      </nav>
      <QueryView q={q} what="the stream">
        {(d) => {
          const s = d.stream;
          if (!s) return <Empty>The stream {name} was not seen in this range.</Empty>;

          const { text: cons, short } = consumerCount(s);
          return (
            <>
              <div className="page-h">
                <HealthIcon level={s.status} />
                <h1 className="mono">{name}</h1>
                <span className="aside">
                  R{s.replicas ?? "–"}
                  {s.serversDisagree && ", servers disagree"}
                </span>
              </div>
              <div className="stats">
                <Stat label="Messages" value={<Num v={s.messages} />} />
                <Stat label="Bytes" value={<Num v={s.bytes} fmt={fmtBytes} />} sub={s.bytesPerDay !== undefined ? `${fmtGrowth(s.bytesPerDay)} per day` : undefined} />
                <Stat label="Consumers" value={cons} bad={short} sub={s.consumersExpected !== undefined ? "present / expected" : undefined} />
                <Stat label="Leader" value={s.leader ? <RLink to={serverPath(s.leader)}>{s.leader}</RLink> : <Dash />} />
              </div>
              <section className="sec grid3">
                <QuerySeriesChart q={msgs} title="Messages" start={start} end={end} windows={windows} />
                <QuerySeriesChart q={bytes} title="Bytes" start={start} end={end} windows={windows} />
                <SeriesChart title="Bytes, 7 days" series={s.bytes7d} start={end - 7 * 86_400_000} end={end} error={q.error ? errorText(q.error) : undefined} updatedAt={q.dataUpdatedAt} />
              </section>
              <EntityFindings entity={entity} />
              <Disagreements items={d.disagreements} />
              <Section title="Per server" aside="What each server that holds the stream reports.">
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
                          Messages
                        </th>
                        <th scope="col" className="num">
                          Bytes
                        </th>
                        <th scope="col" className="num">
                          First seq
                        </th>
                        <th scope="col" className="num">
                          Last seq
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
                          <td className="num"><Num v={v.messages} /></td>
                          <td className="num"><Num v={v.bytes} fmt={fmtBytes} /></td>
                          <td className="num"><Num v={v.firstSeq} /></td>
                          <td className="num"><Num v={v.lastSeq} /></td>
                          <td className="mono">{v.created !== undefined ? isoMinute(v.created) : <Dash />}</td>
                          <td className="mono">{v.configHash || <Dash />}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </Section>
              <Section title="Configuration" aside="From the leader's view.">
                <div className="card box-card">{d.config.length ? <KV items={d.config} /> : <p className="muted-note">Not reported.</p>}</div>
              </Section>
              <Section title="Consumers">
                {d.consumers.length ? <ConsumersTable consumers={d.consumers} end={end} showStream={false} /> : <Empty>No consumers.</Empty>}
              </Section>
              <EntityEvents entity={entity} query={`stream=${encodeURIComponent(entity.key)}`} />
            </>
          );
        }}
      </QueryView>
    </>
  );
}

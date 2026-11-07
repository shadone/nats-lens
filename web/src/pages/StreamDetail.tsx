// One stream: numbers, series over the range and 7 days, findings, each
// server's view and where they disagree, configuration, consumers, events.
import { Navigate, useLocation, useParams } from "react-router";
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
import { bucketResource, consumerCount, serverPath, streamPath, withAccountName } from "../lib/view";

export function StreamDetail() {
  const p = useParams();
  const loc = useLocation();
  const account = p.account ?? "";
  const prefix = loc.pathname.startsWith("/kv-stores/") ? "KV_" : "OBJ_";
  const name = p.stream ?? `${prefix}${p.bucket ?? ""}`;
  const resource = bucketResource(name);
  const diagnostics = new URLSearchParams(loc.search).get("diagnostics") === "stream";
  if (p.stream && resource && !diagnostics) {
    return <Navigate replace to={streamPath(account, name) + loc.search + loc.hash} />;
  }
  return <StreamDetailContent account={account} name={name} bucket={diagnostics ? undefined : resource} />;
}

function StreamDetailContent({ account, name, bucket }: { account: string; name: string; bucket?: ReturnType<typeof bucketResource> }) {
  const displayName = bucket?.bucket ?? name;
  usePageTitle(bucket ? `${bucket.label}: ${displayName}` : name);
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
        <RLink to={bucket?.list ?? "/streams"}>{bucket?.kind === "kv" ? "KV Stores" : bucket ? "Object Stores" : "Streams"}</RLink>
        <span aria-hidden="true">/</span>
        <AccountName id={account} name={q.data?.stream?.entity.accountName} />
      </nav>
      <QueryView q={q} what={bucket ? "the bucket" : "the stream"}>
        {(d) => {
          const s = d.stream;
          if (!s) return <Empty>The {bucket ? "bucket" : "stream"} {displayName} was not seen in this range.</Empty>;

          const { text: cons, short } = consumerCount(s);
          return (
            <>
              <div className="page-h">
                <HealthIcon level={s.status} />
                <h1 className="mono">{displayName}</h1>
                <span className="aside">
                  {bucket && `${bucket.label} · `}R{s.replicas ?? "–"}
                  {s.serversDisagree && ", servers disagree"}
                </span>
              </div>
              {bucket && <p className="muted-note">NATS Lens observes bucket storage and replica metadata. It does not read key values or object contents.</p>}
              <div className="stats">
                <Stat label={bucket?.kind === "kv" ? "Stored records" : bucket ? "Stored messages" : "Messages"} value={<Num v={s.messages} />} />
                <Stat label="Bytes" value={<Num v={s.bytes} fmt={fmtBytes} />} sub={s.bytesPerDay !== undefined ? `${fmtGrowth(s.bytesPerDay)} per day` : undefined} />
                <Stat label={bucket?.kind === "kv" ? "Watchers (consumers)" : "Consumers"} value={cons} bad={short} sub={s.consumersExpected !== undefined ? "present / expected" : undefined} />
                {bucket && <Stat label="TTL" value={d.config.find((c) => c.key === "max_age")?.value ?? <Dash />} />}
                {bucket?.kind === "kv" && <Stat label="History per key" value={d.config.find((c) => c.key === "max_msgs_per_subject")?.value ?? <Dash />} />}
                <Stat label="Leader" value={s.leader ? <RLink to={serverPath(s.leader)}>{s.leader}</RLink> : <Dash />} />
              </div>
              <section className="sec grid3">
                <QuerySeriesChart q={msgs} title={bucket?.kind === "kv" ? "Stored records" : bucket ? "Stored messages" : "Messages"} start={start} end={end} windows={windows} />
                <QuerySeriesChart q={bytes} title="Bytes" start={start} end={end} windows={windows} />
                <SeriesChart title="Bytes, 7 days" series={s.bytes7d} start={end - 7 * 86_400_000} end={end} error={q.error ? errorText(q.error) : undefined} updatedAt={q.dataUpdatedAt} />
              </section>
              {bucket && <p className="muted-note">{bucket.kind === "kv" ? "Stored records include retained revisions and deletion markers; they are not a count of live keys." : "Stored messages include object chunks and metadata; they are not a count of objects."}</p>}
              <EntityFindings entity={entity} />
              <Disagreements items={d.disagreements} />
              <Section title="Per server" aside={bucket ? "What each server reports for this bucket’s storage replica." : "What each server that holds the stream reports."}>
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
              <Section title={bucket ? "Bucket configuration" : "Configuration"} aside="From the leader's view.">
                <div className="card box-card">{d.config.length ? <KV items={bucket ? d.config.filter((c) => !["name", "subjects"].includes(c.key)).map((c) => ({ ...c, key: c.key === "max_age" ? "TTL" : c.key === "max_msgs_per_subject" && bucket.kind === "kv" ? "History per key" : c.key })) : d.config} /> : <p className="muted-note">Not reported.</p>}</div>
              </Section>
              <Section title={bucket?.kind === "kv" ? "Watchers" : bucket ? "Storage consumers" : "Consumers"}>
                {d.consumers.length ? <ConsumersTable consumers={d.consumers} end={end} showStream={false} /> : <Empty>{bucket?.kind === "kv" ? "No watchers observed." : "No consumers."}</Empty>}
              </Section>
              {bucket && <details className="sec"><summary>Underlying stream diagnostics</summary><p>The bucket uses stream <span className="mono">{name}</span>. <RLink to={`/streams/${encodeURIComponent(account)}/${encodeURIComponent(name)}?diagnostics=stream`}>Open stream diagnostics</RLink></p></details>}
              <EntityEvents entity={entity} query={`stream=${encodeURIComponent(entity.key)}`} />
            </>
          );
        }}
      </QueryView>
    </>
  );
}

// The Topology page's tables: leaf node sites worst first, the routes
// inside one cluster, and the mirrors and sources between clusters.
import type { ReactNode } from "react";
import type { TopologyCluster, TopologyLeafnode, TopologyReplication, TopologyRoute } from "../api/topology";
import { fmtBytes, fmtInt, fmtShortDuration, shortWhen } from "../lib/format";
import { clusterLabel, lastHeard, quietLabel, replicationWarns } from "../lib/topologyView";
import { findingPath, serverPath, streamPath } from "../lib/view";
import { AccountName } from "./AccountName";
import { Num, RLink } from "./common";

/** A dash for a value that has no meaning in this row, with the reason for screen readers. */
function Unset({ why }: { why: string }) {
  return (
    <span>
      <span aria-hidden="true">–</span>
      <span className="sr-only">{` ${why}`}</span>
    </span>
  );
}

function Card({ title, right, children, className }: { title: string; right?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={`card tp-card ${className ?? ""}`} aria-label={title}>
      <div className="tp-card-h">
        <h2>{title}</h2>
        {right && <div className="right">{right}</div>}
      </div>
      {children}
    </section>
  );
}

/** A server name that never breaks at its hyphens. */
function Name({ n }: { n: string }) {
  return <span className="tm-nw">{n}</span>;
}

/** Server names, each unbroken, the list wrapping after its commas. */
function Names({ list }: { list: string[] }) {
  return (
    <>
      {list.map((n, k) => (
        <span key={n}>
          {k > 0 && ", "}
          <Name n={n} />
        </span>
      ))}
    </>
  );
}

function ServerLink({ name }: { name: string }) {
  return name ? <RLink to={serverPath(name)}>{name}</RLink> : <Unset why="not known" />;
}

/** leafzFailed: per cluster, its servers whose LEAFZ did not answer; a site of unknown state names those of its own cluster. */
export function LeafTable({ rows, total, at, leafzFailed = {} }: { rows: TopologyLeafnode[]; total: number; at?: number; leafzFailed?: Record<string, string[]> }) {
  return (
    <Card title="Leaf nodes, worst first" className="tp-leaf">
      <div className="table-wrap scroll-cue">
        <table className="tbl">
          <thead>
            <tr>
              <th scope="col">Site</th>
              <th scope="col">Hub</th>
              <th scope="col">RTT</th>
              <th scope="col">Drops 24 h</th>
              <th scope="col">Subs</th>
              <th scope="col">Account</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((l) => {
              // Not connected here, but a server whose LEAFZ failed may hold it: its state is not known.
              const unknown = !l.connected && l.connectionUnknown;
              const why = unknown ? "state unknown" : "not connected";
              const hubs = l.connected && l.hubServers.length > 0 ? l.hubServers : [l.hubServer];
              // "last seen 09:12 on nats-eu-1", or "last hub nats-eu-1" without a time.
              const seen =
                l.lastSeen !== undefined ? (
                  <>
                    last seen {shortWhen(l.lastSeen, at ?? l.lastSeen)}
                    {l.hubServer && (
                      <>
                        {" "}
                        on <Name n={l.hubServer} />
                      </>
                    )}
                  </>
                ) : l.hubServer ? (
                  <>
                    last hub <Name n={l.hubServer} />
                  </>
                ) : null;
              return (
                <tr key={`${l.name}|${l.account}`} className={l.connected ? undefined : unknown ? "tp-unknown" : "warn"}>
                  <th scope="row" className="mono">
                    {l.name}
                    {unknown && (
                      <small>
                        state unknown
                        {(leafzFailed[l.hubCluster] ?? []).length > 0 && (
                          <>
                            : LEAFZ did not answer on <Names list={leafzFailed[l.hubCluster]} />
                          </>
                        )}
                        {seen && <>; {seen}</>}
                      </small>
                    )}
                    {!l.connected && !unknown && (
                      <small>
                        not connected
                        {seen && (l.lastSeen !== undefined ? <>, {seen}</> : <>; {seen}</>)}
                      </small>
                    )}
                  </th>
                  <td className="mono tp-hubs">
                    {hubs.map((h, k) => (
                      <span key={h || k}>
                        {k > 0 && " "}
                        <span className="tm-nw">
                          <ServerLink name={h} />
                          {k < hubs.length - 1 && ","}
                        </span>
                      </span>
                    ))}
                  </td>
                  <td className="mono">{l.connected ? <Num v={l.rttMs} fmt={fmtShortDuration} /> : <Unset why={why} />}</td>
                  <td className={`mono${!l.connected && !unknown && (l.drops24h ?? 0) > 0 ? " tp-warn" : ""}`}>
                    <Num v={l.drops24h} />
                  </td>
                  <td className="mono">{l.connected ? <Num v={l.subscriptions} /> : <Unset why={why} />}</td>
                  <td className="mono">
                    <AccountName id={l.account} name={l.accountName} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {total > rows.length && (
        <p className="tp-foot">
          The {fmtInt(rows.length)} worst of {fmtInt(total)} sites are listed.
        </p>
      )}
    </Card>
  );
}

/** Who reports a route that not both ends report, or why a pair is down. */
function reportNote(r: TopologyRoute): string | undefined {
  if (r.down) return "down: neither server lists a route to the other";
  if (r.reportedBy.length !== 1) return undefined;
  const by = r.reportedBy[0];
  const other = by === r.a ? r.b : r.a;
  return r.oneSided ? `only ${by} reports it; ${other} does not` : `as ${by} reports it; ${other} did not answer ROUTEZ`;
}

export function RouteTable({
  routes,
  clusters,
  picked,
  onPick,
  single,
}: {
  routes: TopologyRoute[];
  clusters: TopologyCluster[];
  picked?: string;
  onPick: (c: string) => void;
  /** The system is one cluster without gateways or leaf nodes. */
  single: boolean;
}) {
  const rows = routes.filter((r) => r.cluster === picked);
  const name = picked === undefined ? "the cluster" : clusterLabel(picked);
  const pick =
    clusters.length > 1 ? (
      <label className="tp-pick">
        <span className="lbl">Cluster</span>
        <select value={picked ?? ""} onChange={(e) => onPick(e.target.value)}>
          {clusters.map((c) => (
            <option key={c.name} value={c.name}>
              {clusterLabel(c.name)}
            </option>
          ))}
        </select>
      </label>
    ) : undefined;
  return (
    <Card title={`Routes inside ${name}`} right={pick} className="tp-routes">
      {rows.length === 0 ? (
        <p className="tp-foot">No routes reported inside {name}.</p>
      ) : (
        <div className="table-wrap scroll-cue">
          <table className="tbl">
            <thead>
              <tr>
                <th scope="col">From → to</th>
                <th scope="col">RTT</th>
                <th scope="col">Pending</th>
                <th scope="col">Drops 1 h</th>
                <th scope="col">Drops 24 h</th>
                <th scope="col">Pool</th>
                <th scope="col">Pinned accounts</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const note = reportNote(r);
                const dropping = (r.drops1h ?? 0) > 0;
                return (
                  <tr key={`${r.a}|${r.b}`} className={r.slow || r.down || dropping ? "warn" : undefined}>
                    <th scope="row" className="mono">
                      <ServerLink name={r.a} /> → <ServerLink name={r.b} />
                      {note && <small>{note}</small>}
                    </th>
                    <td className={`mono${r.slow ? " tp-warn" : ""}`}>
                      <Num v={r.rttMs} fmt={fmtShortDuration} />
                      {r.slow && <span className="tp-tag">Slow</span>}
                    </td>
                    <td className="mono">
                      <Num v={r.pendingBytes} fmt={fmtBytes} />
                    </td>
                    <td className={`mono${dropping ? " tp-warn" : ""}`}>
                      <Num v={r.drops1h} />
                    </td>
                    <td className="mono">
                      <Num v={r.drops24h} />
                    </td>
                    <td className="mono">{r.down ? <Unset why="down" /> : r.pool === undefined ? <span className="tp-none">none</span> : fmtInt(r.pool)}</td>
                    <td className="mono">
                      {r.down ? <Unset why="down" /> : r.pinnedAccounts.length === 0 ? <span className="tp-none">none</span> : r.pinnedAccounts.join(", ")}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {single && <p className="tp-foot">For a single cluster this page shows just the routes; with no gateways or leaf nodes those sections stay hidden.</p>}
    </Card>
  );
}

/** A stream's name, linked to its page. */
function StreamLink({ account, stream }: { account: string; stream: string }) {
  return <RLink to={streamPath(account, stream)}>{stream}</RLink>;
}

/** The streams that mirror or source a stream placed in another cluster, those with a warning first. */
export function ReplicationTable({ rows }: { rows: TopologyReplication[] }) {
  return (
    <Card title="Replication between clusters" className="tp-repl">
      <div className="table-wrap scroll-cue">
        <table className="tbl">
          <thead>
            <tr>
              <th scope="col">From → to</th>
              <th scope="col">Kind</th>
              <th scope="col">Lag</th>
              <th scope="col">Last heard</th>
              <th scope="col">Account</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((r) => {
              const heard = lastHeard(r);
              // No leader's report at all, or one without this copy's figures.
              const unreported = r.leader === "" ? "leader not reported" : "not in the leader's report";
              const warns = replicationWarns(r);
              return (
                <tr key={`${r.account}|${r.stream}|${r.kind}|${r.originStream}|${r.sourceId}`} className={warns ? "warn" : undefined}>
                  <th scope="row" className="mono">
                    <span className="tm-nw">
                      <StreamLink account={r.account} stream={r.originStream} /> on {clusterLabel(r.originCluster)}
                    </span>{" "}
                    →{" "}
                    <span className="tm-nw">
                      <StreamLink account={r.account} stream={r.stream} /> on {clusterLabel(r.cluster)}
                    </span>
                    {r.filterSubject && <small>filter {r.filterSubject}</small>}
                    {r.findingId && (
                      <small>
                        <RLink to={findingPath(r.findingId)}>{r.findingCode}</RLink>
                        {quietLabel(r.findingQuiet, r.findingAccepted)}: {r.findingDetail}
                      </small>
                    )}
                    {!r.findingId && r.error && <small>error reported by the leader: {r.error}</small>}
                  </th>
                  <td>{r.kind === "mirror" ? "Mirror" : "Source"}</td>
                  <td className="mono">{r.lag === undefined ? <Unset why={unreported} /> : fmtInt(r.lag)}</td>
                  <td className="mono">{heard ?? <Unset why={unreported} />}</td>
                  <td className="mono">
                    <AccountName id={r.account} name={r.accountName} />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="tp-foot">Lag and last heard are the copying stream leader&apos;s report; a row is in the warning colour while a stream check fires about it. Mirrors and sources inside one cluster are not listed.</p>
    </Card>
  );
}

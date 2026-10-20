// The Topology page's two tables: leaf node sites worst first, and the
// routes inside one cluster.
import type { ReactNode } from "react";
import type { TopologyCluster, TopologyLeafnode, TopologyRoute } from "../api/topology";
import { fmtBytes, fmtInt, fmtShortDuration, shortWhen } from "../lib/format";
import { clusterLabel } from "../lib/topologyView";
import { serverPath } from "../lib/view";
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

function ServerLink({ name }: { name: string }) {
  return name ? <RLink to={serverPath(name)}>{name}</RLink> : <Unset why="not known" />;
}

export function LeafTable({ rows, total, at, leafzFailed = [] }: { rows: TopologyLeafnode[]; total: number; at?: number; leafzFailed?: string[] }) {
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
              const when = l.lastSeen !== undefined ? `last seen ${shortWhen(l.lastSeen, at ?? l.lastSeen)}` : "";
              const seen = [when, l.hubServer ? `${when ? "" : "last "}on ${l.hubServer}` : ""].filter(Boolean).join(" ");
              return (
                <tr key={`${l.name}|${l.account}`} className={l.connected ? undefined : unknown ? "unknown" : "warn"}>
                  <th scope="row" className="mono">
                    {l.name}
                    {unknown && (
                      <small>
                        state unknown{leafzFailed.length > 0 && `: LEAFZ did not answer on ${leafzFailed.join(", ")}`}
                        {seen && `; ${seen}`}
                      </small>
                    )}
                    {!l.connected && !unknown && <small>not connected{seen && `, ${seen}`}</small>}
                  </th>
                  <td className="mono">
                    {hubs.map((h, k) => (
                      <span key={h || k}>
                        {k > 0 && ", "}
                        <ServerLink name={h} />
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

/** Who reports a route that not both ends report. */
function reportNote(r: TopologyRoute): string | undefined {
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
                <th scope="col">Pool</th>
                <th scope="col">Pinned accounts</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const note = reportNote(r);
                return (
                  <tr key={`${r.a}|${r.b}`} className={r.slow ? "warn" : undefined}>
                    <th scope="row" className="mono">
                      <ServerLink name={r.a} /> → <ServerLink name={r.b} />
                      {note && <small>{note}</small>}
                    </th>
                    <td className={`mono${r.slow ? " tp-warn" : ""}`}>
                      <Num v={r.rttMs} fmt={fmtShortDuration} />
                      {r.slow && <span className="tp-tag">slow</span>}
                    </td>
                    <td className="mono">
                      <Num v={r.pendingBytes} fmt={fmtBytes} />
                    </td>
                    <td className="mono">{r.pool === undefined ? <span className="tp-none">none</span> : fmtInt(r.pool)}</td>
                    <td className="mono">{r.pinnedAccounts.length === 0 ? <span className="tp-none">none</span> : r.pinnedAccounts.join(", ")}</td>
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

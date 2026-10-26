// The Applications page's table: one row per application, with its pods,
// instances, servers, library, round trip, subscriptions, pending bytes,
// reconnects and 24-hour traffic. Every column sorts; the order is kept
// in the address.
import type { ReactNode } from "react";
import type { Application } from "../api/applications";
import { TrafficSpark } from "../charts/TrafficSpark";
import {
  APP_SORT,
  connectionsLink,
  eventsLink,
  hasOpenFinding,
  libraryCell,
  libraryName,
  mixedText,
  placesOf,
  reconnectFinding,
  subLine,
  trafficLabel,
} from "../lib/appsView";
import { fmtBytes, fmtInt, fmtShortDuration } from "../lib/format";
import { sortRows, useSort } from "../lib/sort";
import { findingPath, serverPath } from "../lib/view";
import { AccountName } from "./AccountName";
import { Dash, Num, RLink } from "./common";
import { SortTh } from "./SortHeader";

/** The first items of a list, the rest behind a disclosure, and how many the server did not list. */
function Few({ items, total, shown = 2, what, render }: { items: string[]; total: number; shown?: number; what: string; render: (s: string) => ReactNode }) {
  // One item behind a disclosure takes as much room as the item itself.
  const cut = items.length === shown + 1 ? items.length : shown;
  const head = items.slice(0, cut);
  const rest = items.slice(cut);
  const unlisted = Math.max(0, total - items.length);
  return (
    <span className="ap-few">
      {head.map((s) => (
        <span key={s} className="ap-item">
          {render(s)}
        </span>
      ))}
      {rest.length > 0 && (
        <details className="ap-more">
          <summary>
            +{fmtInt(rest.length + unlisted)} more<span className="sr-only"> {what}</span>
          </summary>
          {rest.map((s) => (
            <span key={s} className="ap-item">
              {render(s)}
            </span>
          ))}
          {unlisted > 0 && <span className="ap-item ap-quiet">{fmtInt(unlisted)} not listed</span>}
        </details>
      )}
      {rest.length === 0 && unlisted > 0 && <span className="ap-item ap-quiet">+{fmtInt(unlisted)} not listed</span>}
    </span>
  );
}

function NameCell({ a, showAccount, label }: { a: Application; showAccount: boolean; label: (id: string, name?: string) => string }) {
  const to = connectionsLink(a);
  return (
    <th scope="row" className="ap-app">
      <span className="ap-name">{to ? <RLink to={to}>{a.name}</RLink> : a.name}</span>
      {a.tooling && (
        <span className="ap-tag">
          tooling<span className="sr-only">: nats-lens's own connections, or clients from a loopback address</span>
        </span>
      )}
      <span className="ap-sub">
        {subLine(a)}
        {showAccount &&
          a.accounts.map((x) => (
            <span key={x.account}>
              <span aria-hidden="true"> · </span>
              <span className="sr-only">, account </span>
              <AccountName id={x.account} name={x.accountName || undefined} label={label(x.account, x.accountName || undefined)} />
            </span>
          ))}
      </span>
      {a.findings.length > 0 && (
        <span className="ap-findings">
          {a.findings.map((f) => (
            <RLink key={f.id} to={findingPath(f.id)} className={`x ap-finding ap-sev-${f.muted ? "muted" : f.severity}`}>
              <span className="mono">{f.checkCode}</span> {f.checkName}
              {f.muted ? " (muted)" : ""}
            </RLink>
          ))}
        </span>
      )}
    </th>
  );
}

function PlacesCell({ a }: { a: Application }) {
  const p = placesOf(a);
  if (p.total === 0) return <td><Dash /></td>;
  return (
    <td className={`ap-places${p.kind === "addresses" ? " ap-addrs" : ""}`}>
      {p.kind === "addresses" && <span className="sr-only">no pod in the names; client addresses: </span>}
      <Few items={p.items} total={p.total} shown={p.kind === "pods" ? 2 : 1} what={p.kind === "pods" ? "pods" : "addresses"} render={(s) => s} />
    </td>
  );
}

function ServersCell({ a }: { a: Application }) {
  const counts = new Map(a.servers.map((s) => [s.server, s.connections]));
  return (
    <td className="ap-servers">
      {a.servers.length === 0 ? (
        <Dash />
      ) : (
        <Few
          items={a.servers.map((s) => s.server)}
          total={a.servers.length}
          shown={3}
          what="servers"
          render={(s) => (
            <RLink to={serverPath(s)} className="x ap-chip">
              {s} <span className="ap-chip-n">{fmtInt(counts.get(s) ?? 0)}</span> <span className="sr-only">{counts.get(s) === 1 ? "connection" : "connections"}</span>
            </RLink>
          )}
        />
      )}
    </td>
  );
}

function LibraryCell({ a }: { a: Application }) {
  const c = libraryCell(a);
  if (!c.text) return <td><Dash /></td>;
  if (!c.mixed) return <td className="ap-lib">{c.text}</td>;
  return (
    <td className="ap-lib">
      <span className="ap-mixed">mixed</span>
      <span className="sr-only">: {mixedText(a.libraries)}</span>
      {a.libraries.map((l) => (
        <span key={`${l.lang}/${l.version}`} className="ap-lib-line" aria-hidden="true">
          {libraryName(l)} <span className="ap-quiet">on {fmtInt(l.connections)}</span>
        </span>
      ))}
    </td>
  );
}

function PendingCell({ a }: { a: Application }) {
  return (
    <td className="num">
      <Num v={a.pendingBytes} fmt={fmtBytes} />
      {a.connections > 1 && a.pendingBytesMax !== undefined && (a.pendingBytes ?? 0) > 0 && <span className="ap-small">max {fmtBytes(a.pendingBytesMax)}</span>}
    </td>
  );
}

function ReconnectsCell({ a }: { a: Application }) {
  if (a.reconnects === undefined)
    return (
      <td className="num">
        <span aria-hidden="true">–</span>
        <span className="sr-only"> no event captured</span>
      </td>
    );
  const f = reconnectFinding(a);
  const to = a.reconnects > 0 ? (f ? findingPath(f.id) : eventsLink(a)) : undefined;
  const text = fmtInt(a.reconnects);
  return (
    <td className={`num${a.reconnects > 0 ? " ap-warn-text" : " ap-quiet"}`}>
      {to ? (
        <RLink to={to} aria-label={f ? `${text}, see ${f.checkCode}` : `${text}, see the events of ${a.name}`}>
          {text}
        </RLink>
      ) : (
        text
      )}
    </td>
  );
}

function TrafficCell({ a }: { a: Application }) {
  return (
    <td className="ap-traffic">
      <TrafficSpark hours={a.traffic} label={trafficLabel(a)} />
      <span className="ap-small" aria-hidden="true">
        {a.inMsgs === undefined && a.outMsgs === undefined ? "not measured" : `${fmtInt(a.inMsgs ?? 0)} / ${fmtInt(a.outMsgs ?? 0)}`}
      </span>
    </td>
  );
}

export function AppTable({ rows, showAccount, label }: { rows: Application[]; showAccount: boolean; label: (id: string, name?: string) => string }) {
  const [sort, by] = useSort();
  // Own keys only: ?sort=__proto__ must not reach the prototype.
  const value = sort && Object.hasOwn(APP_SORT, sort.key) ? APP_SORT[sort.key] : undefined;
  const sorted = value && sort ? sortRows(rows, value, sort.dir) : rows;
  const th = { sort, onSort: by };
  return (
    <section className="card table-wrap scroll-cue ap-table" aria-label="Applications table">
      <table className="tbl ap-tbl">
        <thead>
          <tr>
            <SortTh id="name" label="Application" first="asc" {...th} />
            <SortTh id="pods" label="Pods" {...th} />
            <SortTh id="instances" label="Instances" num {...th} />
            <SortTh id="servers" label="Servers" {...th} />
            <SortTh id="library" label="Library" first="asc" {...th} />
            <SortTh id="rtt" label="RTT p50" num {...th} />
            <SortTh id="subs" label="Subs" num {...th} />
            <SortTh id="pending" label="Pending" num {...th} />
            <SortTh id="reconnects" label="Reconnects 24 h" num {...th} />
            <SortTh id="traffic" label="Msgs in / out 24 h" {...th} />
          </tr>
        </thead>
        <tbody>
          {sorted.map((a) => (
            <tr key={a.name} className={hasOpenFinding(a) ? "ap-warn" : undefined}>
              <NameCell a={a} showAccount={showAccount} label={label} />
              <PlacesCell a={a} />
              <td className="num">{fmtInt(a.instances)}</td>
              <ServersCell a={a} />
              <LibraryCell a={a} />
              <td className="num">
                <Num v={a.rttMs} fmt={fmtShortDuration} />
              </td>
              <td className="num">
                <Num v={a.subscriptions} />
              </td>
              <PendingCell a={a} />
              <ReconnectsCell a={a} />
              <TrafficCell a={a} />
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  );
}

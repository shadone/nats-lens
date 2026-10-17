// Connections: client connections of the newest epoch in the range, by
// server and connection ID, filtered by server, account and name.
import { useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import { useConnections, useServers } from "../api/queries";
import { Dash, Empty, QueryView, RLink, Num } from "../components/common";
import { Pager, usePager } from "../components/EntityParts";
import { fmtBytes, fmtDuration, fmtShortDuration } from "../lib/format";
import { usePageTitle } from "../lib/usePageTitle";
import { useSystem } from "../lib/useSystem";
import { rangeKey } from "../lib/timeRange";
import { useResolvedRange, useTimeRange } from "../lib/useTimeRange";
import { AccountName } from "../components/AccountName";
import { accountLabeller, serverPath } from "../lib/view";

export function Connections() {
  usePageTitle("Connections");
  const [params, setParams] = useSearchParams();
  const { spec } = useTimeRange();
  const { end } = useResolvedRange(spec);
  const server = params.get("server") ?? "";
  const account = params.get("account") ?? "";
  const name = params.get("name") ?? "";
  const tooling = params.get("tooling") === "1";
  const [nameText, setNameText] = useState(name);
  const [accountText, setAccountText] = useState(account);
  const servers = useServers(spec).data?.servers ?? [];
  const pager = usePager([useSystem(), server, account, name, String(tooling), rangeKey(spec)].join("|"));
  // A name filter shows the tooling connections it matches: whoever types
  // a name looks for that connection.
  const q = useConnections(spec, { server, account, nameContains: name, includeTooling: tooling || name !== "", pageSize: 100, pageToken: pager.token });
  const set = (entries: [string, string][]) =>
    setParams((p) => {
      const n = new URLSearchParams(p);
      for (const [k, v] of entries) {
        if (v) n.set(k, v);
        else n.delete(k);
      }
      return n;
    });
  const apply = (e: FormEvent) => {
    e.preventDefault();
    set([
      ["name", nameText.trim()],
      ["account", accountText.trim()],
    ]);
  };
  return (
    <>
      <div className="page-h">
        <h1>Connections</h1>
        <span className="aside">Client connections in the newest epoch of the range.</span>
      </div>
      <div className="filters">
        <label className="grp">
          <span className="lbl">Server</span>
          <select value={server} onChange={(e) => set([["server", e.target.value]])}>
            <option value="">All servers</option>
            {servers.map((s) => (
              <option key={s.name} value={s.name}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
        <form className="grp wide" onSubmit={apply} role="search" aria-label="Filter connections">
          <input type="search" placeholder="name contains" aria-label="Connection name contains" value={nameText} onChange={(e) => setNameText(e.target.value)} />
          <input type="search" placeholder="account" aria-label="Account" value={accountText} onChange={(e) => setAccountText(e.target.value)} />
          <button type="submit" className="btn">
            Filter
          </button>
        </form>
        <label className="check">
          <input type="checkbox" checked={tooling} onChange={(e) => set([["tooling", e.target.checked ? "1" : ""]])} /> Include tooling
        </label>
      </div>
      <QueryView q={q} what="connections">
        {(page) => (
          <>
            {!tooling && name !== "" && page.items.some((c) => c.tooling) && (
              <p className="muted-note">Tooling connections are shown because the name filter matches them.</p>
            )}
            {page.toolingHidden > 0 && (
              <p className="muted-note">
                {page.toolingHidden} tooling {page.toolingHidden === 1 ? "connection" : "connections"} hidden: nats-lens's own, or clients from a loopback address.
              </p>
            )}
          {page.items.length === 0 ? (
            <Empty>No connections match in the newest epoch of this range.</Empty>
          ) : (
            <div className="card table-wrap">
              <table className="tbl">
                <thead>
                  <tr>
                    <th scope="col">Name</th>
                    <th scope="col">Server / CID</th>
                    <th scope="col">Client</th>
                    <th scope="col">Account, user</th>
                    <th scope="col">Address</th>
                    <th scope="col" className="num">
                      Up
                    </th>
                    <th scope="col" className="num">
                      Idle
                    </th>
                    <th scope="col" className="num">
                      RTT
                    </th>
                    <th scope="col" className="num">
                      Pending
                    </th>
                    <th scope="col" className="num">
                      Msgs in / out
                    </th>
                    <th scope="col" className="num">
                      Stalls
                    </th>
                    <th scope="col" className="num">
                      Subs
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {page.items.map((c, _i, all) => (
                    <tr key={c.key}>
                      <td className="mono">
                        {c.name ? <RLink to={`/events?client=${encodeURIComponent(c.name)}`}>{c.name}</RLink> : <Dash />}
                        {c.tooling && (
                          <small>
                            <span aria-hidden="true"> · </span>
                            tooling<span className="sr-only">: nats-lens's own connection, or a client from a loopback address</span>
                          </small>
                        )}
                      </td>
                      <td className="mono">
                        <RLink to={serverPath(c.server)}>{c.server}</RLink> / {c.cid}
                      </td>
                      <td className="mono">
                        {c.lang} {c.version}
                      </td>
                      <td className="mono">
                        <AccountName id={c.account} name={c.accountName} label={accountLabeller(all)(c.account, c.accountName)} />
                        <small>{c.user}</small>
                      </td>
                      <td className="mono">
                        {c.ip}{c.port !== undefined && `:${c.port}`}
                      </td>
                      <td className="num">{c.startTime !== undefined ? fmtDuration(end - c.startTime) : <Dash />}</td>
                      <td className="num">{c.lastActivity !== undefined ? fmtDuration(Math.max(0, end - c.lastActivity)) : <Dash />}</td>
                      <td className="num">{c.rttMs !== undefined ? fmtShortDuration(c.rttMs) : <Dash />}</td>
                      <td className={`num${(c.pendingBytes ?? 0) > 0 ? " bad" : ""}`}><Num v={c.pendingBytes} fmt={fmtBytes} /></td>
                      <td className="num">
                        <Num v={c.inMsgs} /> / <Num v={c.outMsgs} />
                      </td>
                      <td className={`num${(c.stalls ?? 0) > 0 ? " bad" : ""}`}><Num v={c.stalls} /></td>
                      <td className="num"><Num v={c.subscriptions} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <Pager page={pager.page} next={page.nextPageToken} onNext={() => pager.next(page.nextPageToken)} onPrev={pager.prev} total={page.totalCount} shown={page.items.length} />
            </div>
          )}
          </>
        )}
      </QueryView>
    </>
  );
}

import type { ConnectionsModel } from "../pages/Connections";
// Connections: client connections of the newest epoch in the range, by
// server and connection ID, filtered by server, account and name.
import { connectionFindings } from "../api/connectionFindings";
import { AccountName } from "../components/AccountName";
import { Dash, Empty, Num, QueryView, RLink } from "../components/common";
import { Pager } from "../components/Pager";
import { fmtBytes, fmtDuration, fmtShortDuration } from "../lib/format";
import { accountLabeller, findingPath, serverPath } from "../lib/view";

export function ConnectionsView({ model }: { model: ConnectionsModel }) {
  const { end, server, name, tooling, nameText, setNameText, accountText, setAccountText, pager, q, findings, findingData, servers, set, apply } = model;

  return (
    <>
      <div className="page-h">
        <span className="aside">Client connections in the newest collection of the range.</span>
      </div>
      <p className="muted-note">Findings show state at the range’s end, including resolved and muted findings. RTT is from the newest collection. RTT findings cover the service name before @. The RTT check follows configured persistence rules across collections; a high sample may have no finding.</p>
      {findings.error && <p className="muted-note" role="status">Finding status is unknown: could not load findings. <button className="btn" onClick={() => void findings.refetch()}>Retry findings</button></p>}
      {findingData && (findingData.nextPageToken || findingData.totalCount > findingData.items.length) && <p className="muted-note" role="status">Finding results are incomplete. Unmatched connections have unknown status. <RLink to="/findings">Browse findings</RLink></p>}
      <div className="filters">
        <label className="grp">
          <span className="lbl">Server</span>
          <select value={server} onChange={(e) => set([["server", e.target.value]])}>
            <option value="">All servers</option>
            {servers.map((s) => (
              <option key={s} value={s}>
                {s}
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
            <Empty>No connections match in the newest collection of this range.</Empty>
          ) : (
            <div className="card table-wrap">
              <table className="tbl">
                <thead>
                  <tr>
                    <th scope="col">Name</th>
                    <th scope="col">Findings in range</th>
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
                      <td>
                        {!findingData ? <span>Unknown{findings.isPending ? " (loading)" : ""}</span> : (() => {
                          const matches = connectionFindings(findingData.items, c.name);
                          if (!matches.length) return <span>{findingData.nextPageToken || findingData.totalCount > findingData.items.length ? "Unknown (incomplete)" : "No matching finding"}</span>;
                          // ASVS 1.2.1 / 1.2.2: React escapes labels; findingPath encodes the ID.
                          return matches.map(f => <small key={f.id}><RLink to={findingPath(f.id)} title={f.checkName}>{f.checkCode} · {f.state[0].toUpperCase() + f.state.slice(1)}{f.mute ? " · Muted" : ""}</RLink></small>);
                        })()}
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

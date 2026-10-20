// The Accounts page's three cards: the accounts with their limits, the
// failed logins of the 24 hours before the collection, and what expires
// soon.
import type { ReactNode } from "react";
import type { AccountRow, AccountsAndAuth, Expiring } from "../api/accounts";
import { dayMonHm, fmtBytes, fmtInt, isoDate, shortWhen } from "../lib/format";
import {
  accountTitle,
  connLimitText,
  diskLimitText,
  expiresIn,
  expiringCountLine,
  expiringTone,
  expiringWhat,
  importsExports,
  jetStreamOff,
  jwtCell,
  limitBar,
  loginHot,
  loginUser,
  loginWindowMs,
  loginsCutLine,
  orderAccounts,
  shortIdentifier,
  type LimitBar,
  type Tone,
} from "../lib/accountsView";
import { serverPath } from "../lib/view";
import { Num, RLink } from "./common";

const DAY = 86_400_000;

/** A dash for a value that is not there, with the reason for screen readers. */
function Unset({ why }: { why: string }) {
  return (
    <span className="ac-none">
      <span aria-hidden="true">–</span>
      <span className="sr-only">{` ${why}`}</span>
    </span>
  );
}

function Card({ title, sub, children, className }: { title: string; sub?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={`card ac-card ${className ?? ""}`} aria-label={title}>
      <div className="ac-card-h">
        <h2>{title}</h2>
        {sub && <p className="ac-sub">{sub}</p>}
      </div>
      {children}
    </section>
  );
}

function toneClass(t: Tone | undefined): string {
  return t ? ` ac-${t}` : "";
}

/** The Connections list scoped to the account: the account filter other pages offer. */
function accountPath(account: string): string {
  return `/connections?account=${encodeURIComponent(account)}`;
}

function Bar({ bar, label }: { bar: LimitBar; label: string }) {
  return (
    <div className="ac-bar" role="img" aria-label={`${label}: ${Math.round(bar.pct)}% of the limit`}>
      <div className={`ac-bar-fill${bar.tone === "warn" ? " ac-bar-warn" : ""}`} style={{ width: `${bar.pct}%` }} />
    </div>
  );
}

function AccountCell({ a }: { a: AccountRow }) {
  const id = shortIdentifier(a);
  return (
    <th scope="row">
      <RLink to={accountPath(a.account)} className="x ac-name" title={a.account}>
        {accountTitle(a)}
      </RLink>
      {id && (
        <span className="ac-id" title={a.account}>
          <span aria-hidden="true">{id}</span>
          <span className="sr-only">{` account ID ${a.account}`}</span>
        </span>
      )}
    </th>
  );
}

function ConnectionsCell({ a }: { a: AccountRow }) {
  if (a.connections === undefined) return <Unset why="not reported" />;
  const of = connLimitText(a.connLimit);
  const bar = limitBar(a.connections, a.connLimit);
  return (
    <>
      <div className={bar?.tone === "warn" ? "ac-warn" : undefined}>
        {fmtInt(a.connections)}
        {of && <span className="ac-of"> {of}</span>}
      </div>
      {bar && <Bar bar={bar} label="Connections" />}
      {(a.leafnodes ?? 0) > 0 && <small>{a.leafnodes === 1 ? "1 leaf node" : `${fmtInt(a.leafnodes!)} leaf nodes`}</small>}
    </>
  );
}

function DiskCell({ a }: { a: AccountRow }) {
  if (jetStreamOff(a) && !a.jsStorage) return <span className="ac-none">JetStream off</span>;
  if (a.jsStorage === undefined) return <Unset why="not reported" />;
  const of = diskLimitText(a);
  const bar = limitBar(a.jsStorage, a.diskLimit);
  return (
    <>
      <div className={bar?.tone === "warn" ? "ac-warn" : undefined}>
        {fmtBytes(a.jsStorage)}
        {of && <span className="ac-of"> {of}</span>}
      </div>
      {bar && <Bar bar={bar} label="JetStream disk" />}
      {jetStreamOff(a) && <small>JetStream off</small>}
    </>
  );
}

export function AccountsTable({ d }: { d: AccountsAndAuth }) {
  const rows = orderAccounts(d.accounts);
  return (
    <section className="card ac-card ac-accounts" aria-label="Accounts and limits">
      {rows.length === 0 ? (
        <p className="ac-foot">No account reported in this collection.</p>
      ) : (
        <div className="table-wrap scroll-cue">
          <table className="tbl">
            <thead>
              <tr>
                <th scope="col">Account</th>
                <th scope="col">Connections</th>
                <th scope="col">Subscriptions</th>
                <th scope="col">JetStream disk</th>
                <th scope="col">Streams</th>
                <th scope="col">Imports / exports</th>
                <th scope="col">JWT expires</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((a) => {
                const ie = importsExports(a);
                const jwt = jwtCell(a, d.operatorMode, d.epochAt);
                return (
                  <tr key={a.account}>
                    <AccountCell a={a} />
                    <td className="mono">
                      <ConnectionsCell a={a} />
                    </td>
                    <td className="mono">
                      <Num v={a.subscriptions} />
                    </td>
                    <td className="mono">
                      <DiskCell a={a} />
                    </td>
                    <td className="mono">
                      <Num v={a.streams} />
                    </td>
                    <td className={a.isSystem ? "ac-quiet" : undefined}>{ie ?? <Unset why="not looked up yet" />}</td>
                    <td className={`mono${toneClass(jwt.tone)}`}>{jwt.text ?? <Unset why={jwt.why ?? "not known"} />}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {d.accountsTotal > d.accounts.length && (
        <p className="ac-foot">
          The first {fmtInt(d.accounts.length)} of {fmtInt(d.accountsTotal)} accounts are listed.
        </p>
      )}
    </section>
  );
}

export function FailedLoginsCard({ d }: { d: AccountsAndAuth }) {
  const windowMs = loginWindowMs(d);
  const short = d.failedLoginsWindowStart !== undefined && windowMs < DAY - 60_000;
  const cut = loginsCutLine(d);
  return (
    <Card title="Failed logins, 24 h" sub="From the servers' auth error advisories, grouped by user, address and reason." className="ac-logins">
      {d.failedLogins.length === 0 ? (
        <p className="ac-empty">No failed logins in these 24 hours.</p>
      ) : (
        <div className="table-wrap scroll-cue">
          <table className="tbl">
            <thead>
              <tr>
                <th scope="col">User</th>
                <th scope="col">From</th>
                <th scope="col">Reason</th>
                <th scope="col">Count</th>
                <th scope="col">Last</th>
              </tr>
            </thead>
            <tbody>
              {d.failedLogins.map((l) => {
                const hot = loginHot(l, windowMs);
                return (
                  <tr key={`${l.user}|${l.host}|${l.reason}`} className={hot ? "ac-hot" : undefined}>
                    <th scope="row" className={`ac-mono${l.user ? "" : " ac-quiet"}`}>
                      {loginUser(l)}
                      {l.account && (
                        <small>
                          account <RLink to={accountPath(l.account)}>{accountTitle(l)}</RLink>
                        </small>
                      )}
                    </th>
                    <td className="mono">{l.host || <Unset why="address not reported" />}</td>
                    <td className="ac-reason">{l.reason || <Unset why="no reason given" />}</td>
                    <td className={`mono${hot ? " ac-warn" : ""}`}>{fmtInt(l.count)}</td>
                    <td className="mono">{l.last === undefined ? <Unset why="not known" /> : shortWhen(l.last, d.epochAt ?? l.last)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <div className="ac-notes">
        {cut && <p>{cut}</p>}
        {d.failedLogins.length > 0 && <p className="ac-text">A client retrying with revoked credentials every minute shows up here before anyone notices it never connected.</p>}
        {short && <p>The history starts at {dayMonHm(d.failedLoginsWindowStart!)} UTC, so fewer than 24 hours are counted.</p>}
        {d.captureGaps && <p className="ac-gap">Event capture was not running for part of these 24 hours, so the counts can be low.</p>}
      </div>
    </Card>
  );
}

function ExpiringName({ e }: { e: Expiring }) {
  switch (e.kind) {
    case "certificate":
      return <RLink to={serverPath(e.server)}>{e.server}</RLink>;
    case "accountJwt":
      return <RLink to={accountPath(e.account)}>{accountTitle(e)}</RLink>;
    case "userJwt":
      return <>{e.user || "(no user)"}</>;
    default:
      return null;
  }
}

export function ExpiringCard({ d }: { d: AccountsAndAuth }) {
  const count = expiringCountLine(d);
  return (
    <Card
      title="Expiring soon"
      sub={d.operatorMode ? "User and account JWTs and server certificates, within 30 days." : "Server certificates, within 30 days."}
      className="ac-expiring"
    >
      {d.expiring.length === 0 ? (
        <p className="ac-empty">Nothing expires within 30 days.</p>
      ) : (
        <div className="table-wrap scroll-cue">
          <table className="tbl">
            <thead>
              <tr>
                <th scope="col">What</th>
                <th scope="col">Expires</th>
                <th scope="col">In</th>
              </tr>
            </thead>
            <tbody>
              {d.expiring.map((e) => {
                const tone = expiringTone(e, d.epochAt);
                return (
                  <tr key={`${e.kind}|${e.server}|${e.listener}|${e.account}|${e.user}`}>
                    <th scope="row">
                      <span className="ac-mono">
                        <ExpiringName e={e} />
                      </span>{" "}
                      {expiringWhat(e)}
                      {e.kind === "userJwt" && (
                        <small>
                          account <RLink to={accountPath(e.account)}>{accountTitle(e)}</RLink>
                          {e.connections > 0 && ` · ${e.connections === 1 ? "1 connection" : `${fmtInt(e.connections)} connections`}`}
                        </small>
                      )}
                    </th>
                    <td className="mono">{e.expires === undefined ? <Unset why="no time reported" /> : isoDate(e.expires)}</td>
                    <td className={`mono${toneClass(tone)}`}>{expiresIn(e, d.epochAt)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <div className="ac-notes">
        {count && <p>{count}</p>}
        {!d.operatorMode && <p>In config mode there are no JWTs, so this card lists server certificates only, and accounts come from the server configuration.</p>}
      </div>
    </Card>
  );
}

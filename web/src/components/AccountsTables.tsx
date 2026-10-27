// The Accounts page's three cards: the accounts with their limits, the
// failed logins of the 24 hours before the collection, and what expires
// soon.
import type { ReactNode } from "react";
import type { AccountRow, AccountsAndAuth, Expiring } from "../api/accounts";
import { dayMonHm, fmtBytes, fmtInt, isoDate, shortWhen } from "../lib/format";
import {
  accountTitle,
  connectionsUsed,
  connLimitText,
  EVENTS_NOT_CAPTURED_TEXT,
  diskLimitText,
  expiresIn,
  expiringCountLine,
  expiringTone,
  expiringWhat,
  importsExports,
  diskStateText,
  jwtCell,
  leafNodeParts,
  limitBar,
  limitBarParts,
  limitBarText,
  loginHot,
  loginRateText,
  loginUser,
  loginWindowMs,
  loginWindowShort,
  loginsCutLine,
  orderAccounts,
  seenAtParts,
  shortIdentifier,
  type LimitBar,
  type Tone,
} from "../lib/accountsView";
import { serverPath } from "../lib/view";
import { Num, RLink } from "./common";

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

/** One part of a line under a cell, kept on one line: the line breaks only between parts. */
function Nw({ children }: { children: ReactNode }) {
  return <span className="ac-nw">{children}</span>;
}

/**
 * Parts joined by sep, each kept on one line. A separator that starts with
 * a space (" · ") keeps its mark on the line of the part after it, so a
 * line breaks at that space and never ends with the dot; ", " and "; "
 * break after their space as text does.
 */
function Parts({ parts, sep }: { parts: string[]; sep: string }) {
  const leads = sep.startsWith(" ");
  return (
    <>
      {parts.map((p, k) => (
        <span key={p}>
          {k > 0 && (leads ? " " : sep)}
          <Nw>
            {leads && k > 0 ? `${sep.trimStart()}` : ""}
            {p}
          </Nw>
        </span>
      ))}
    </>
  );
}

function toneClass(t: Tone | undefined): string {
  return t ? ` ac-${t}` : "";
}

/** The Connections list scoped to the account: the account filter other pages offer. */
function accountPath(account: string): string {
  return `/connections?account=${encodeURIComponent(account)}`;
}

/**
 * A bar against a limit, named with the real share. From 80% a line under
 * it says the share in words, so the warning does not rest on colour alone;
 * screen readers hear it from the bar's name.
 */
function Bar({ bar, label }: { bar: LimitBar; label: string }) {
  const fill = `ac-bar-fill${bar.pct > 0 ? " ac-bar-some" : ""}${bar.tone === "ok" ? "" : ` ac-bar-${bar.tone}`}`;
  return (
    <>
      <div className="ac-bar" role="img" aria-label={`${label}: ${limitBarText(bar)}`}>
        <div className={fill} style={{ width: `${bar.pct}%` }} />
      </div>
      {bar.tone !== "ok" && (
        <small className="ac-limit-note" aria-hidden="true">
          <Parts parts={limitBarParts(bar)} sep=", " />
        </small>
      )}
    </>
  );
}

/** The class of a count against its bar: the bar's tone, none below 80%. */
function barTone(bar: LimitBar | undefined): string | undefined {
  return bar && bar.tone !== "ok" ? `ac-${bar.tone}` : undefined;
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

/**
 * The connections as nats-server counts them against the limit: every
 * server's clients plus the leaf nodes of the server that holds the most,
 * named under the count, so a bar at 100% and account-001 agree.
 */
function ConnectionsCell({ a }: { a: AccountRow }) {
  const used = connectionsUsed(a);
  if (used === undefined) return <Unset why="not reported" />;
  const of = connLimitText(a.connLimit);
  const bar = limitBar(used, a.connLimit);
  const leafs = leafNodeParts(a);
  return (
    <>
      <div className={barTone(bar)}>
        {fmtInt(used)}
        {of && <span className="ac-of"> {of}</span>}
      </div>
      {bar && <Bar bar={bar} label="Connections" />}
      {leafs && (
        <small>
          <Parts parts={leafs} sep=" · " />
        </small>
      )}
    </>
  );
}

function DiskCell({ a }: { a: AccountRow }) {
  const state = diskStateText(a);
  if (state && !a.jsStorage) return <span className="ac-none">{state}</span>;
  if (a.jsStorage === undefined) return <Unset why="not reported" />;
  const of = diskLimitText(a);
  const bar = limitBar(a.jsStorage, a.diskLimit);
  return (
    <>
      <div className={barTone(bar)}>
        {fmtBytes(a.jsStorage)}
        {of && <span className="ac-of"> {of}</span>}
      </div>
      {bar && <Bar bar={bar} label="JetStream disk" />}
      {state && <small>{state}</small>}
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
                    <td className={a.isSystem || !a.detailsKnown ? "ac-quiet" : undefined}>{ie ?? <Unset why="not reported" />}</td>
                    <td className={`mono${toneClass(jwt.tone)}`}>
                      {jwt.text ?? <Unset why={jwt.why ?? "not known"} />}
                      {jwt.note && (
                        <small>
                          <Nw>{jwt.note}</Nw>
                        </small>
                      )}
                    </td>
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
  const short = loginWindowShort(d);
  const since = short ? `${dayMonHm(d.failedLoginsWindowStart!)} UTC` : "";
  const cut = loginsCutLine(d);
  return (
    <Card title="Failed logins, 24 h" sub="From the servers' auth error advisories, grouped by user, address and reason." className="ac-logins">
      {!d.eventsCaptured ? (
        <p className="ac-empty">{EVENTS_NOT_CAPTURED_TEXT}</p>
      ) : d.failedLogins.length === 0 ? (
        <p className="ac-empty">{short ? `No failed logins since ${since}.` : "No failed logins in these 24 hours."}</p>
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
                      {(l.account || hot) && (
                        <small>
                          {l.account && (
                            <Nw>
                              account <RLink to={accountPath(l.account)}>{accountTitle(l)}</RLink>
                            </Nw>
                          )}
                          {l.account && hot && " · "}
                          {hot && (
                            <Nw>
                              <span className="sr-only">frequent, </span>
                              {loginRateText(l, windowMs, d.failedLoginsWindowStart)}
                            </Nw>
                          )}
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
      {d.eventsCaptured && (
        <div className="ac-notes">
          {cut && <p>{cut}</p>}
          {d.failedLogins.length > 0 && <p className="ac-text">A client retrying with revoked credentials every minute shows up here before anyone notices it never connected.</p>}
          {short && d.failedLogins.length > 0 && <p>The history starts at {since}, so fewer than 24 hours are counted.</p>}
          {d.captureGaps && <p className="ac-gap">Event capture stopped for part of {short ? "this window" : "these 24 hours"}, so the counts can be low.</p>}
        </div>
      )}
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
                const seen = seenAtParts(e, d.epochAt);
                return (
                  <tr key={`${e.kind}|${e.server}|${e.listener}|${e.account}|${e.user}`}>
                    <th scope="row">
                      <span className="ac-mono">
                        <ExpiringName e={e} />
                      </span>{" "}
                      {expiringWhat(e)}
                      {seen && (
                        <small>
                          <Parts parts={seen} sep="; " />
                        </small>
                      )}
                      {e.kind === "userJwt" && (
                        <small>
                          <Nw>
                            account <RLink to={accountPath(e.account)}>{accountTitle(e)}</RLink>
                          </Nw>
                          {e.connections > 0 && (
                            <>
                              {" · "}
                              <Nw>{e.connections === 1 ? "1 connection" : `${fmtInt(e.connections)} connections`}</Nw>
                            </>
                          )}
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

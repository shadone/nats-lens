// The Subjects page's pattern table: one row per stream (every subject of
// it in the first cell), per subscribed subject, per account's reply
// inboxes and per account's JetStream API, rows with a warning first.
import { Fragment } from "react";
import type { Delivered, SubjectRow, Subjects } from "../api/subjects";
import { fmtInt } from "../lib/format";
import { connectionRateText, connectionWhere, cutText, hasDelivered, isStored, measuredText, moreRatesText, note, rateText, subscribers } from "../lib/subjectsView";
import { streamLabel, accountLabeller, streamPath } from "../lib/view";
import { AccountName } from "./AccountName";
import { Dash, RLink } from "./common";

const enc = encodeURIComponent;

function rowKey(r: SubjectRow): string {
  return `${r.account}|${r.kind}|${r.stream}|${r.patterns.join(" ")}|${r.queue}`;
}

/** The Consumers list of the stream, with its ephemeral consumers when it has any. */
function consumersPath(r: SubjectRow): string {
  return `/consumers?stream=${enc(`${r.account}/${r.stream}`)}${r.ephemeral > 0 ? "&ephemeral=1" : ""}`;
}

/** The Connections list filtered by a connection's name. */
export function connectionPath(name: string): string {
  return `/connections?name=${enc(name)}`;
}

/** "(a, b, c +2)" with each name a link to its connections. */
export function Names({ names, more }: { names: string[]; more: number }) {
  if (names.length === 0 && more === 0) return null;
  const last = names.length - 1;
  return (
    <span className="sj-names">
      {" "}
      {names.length === 0 && `(+${fmtInt(more)})`}
      {names.map((n, i) => (
        <Fragment key={n}>
          {i > 0 && " "}
          {/* A name breaks only when it is wider than its box; its brackets, comma and "+N" stay with it. */}
          <span className="sj-name">
            {i === 0 && "("}
            <RLink to={connectionPath(n)}>{n}</RLink>
            {i < last ? "," : more > 0 ? ` +${fmtInt(more)})` : ")"}
          </span>
        </Fragment>
      ))}
    </span>
  );
}

/**
 * The connections of a row or a tester's match with their delivered
 * rates, behind a disclosure: shown when there is more than one, as one
 * connection's rate is the row's.
 */
export function ConnectionRates({ d, label = "Per connection" }: { d: Delivered; label?: string }) {
  if (d.rates.length + d.moreRates < 2) return null;
  const more = moreRatesText(d);
  return (
    <details className="sj-rates">
      <summary>{label}</summary>
      <ul>
        {d.rates.map((c) => (
          <li key={`${c.server}|${c.cid}`}>
            {c.name ? <RLink to={connectionPath(c.name)}>{c.name}</RLink> : "unnamed"}
            <span className="sj-rates-where"> {connectionWhere(c)}</span>:{" "}
            <span className={c.perSecond === undefined ? "sj-quiet" : "sj-rates-v"}>{connectionRateText(c)}</span>
          </li>
        ))}
        {more && <li className="sj-quiet">{more}</li>}
      </ul>
    </details>
  );
}

function Subscribers({ r }: { r: SubjectRow }) {
  const s = subscribers(r);
  return (
    <>
      {s.consumers ? <RLink to={consumersPath(r)}>{s.lead}</RLink> : <span className={s.lead === "none" ? "sj-quiet" : undefined}>{s.lead}</span>}
      {s.queue && (
        <>
          {" "}
          <span className="sj-name">{s.queue}</span>
        </>
      )}
      <Names names={s.names} more={s.more} />
      {hasDelivered(r) && <ConnectionRates d={r.delivered} />}
    </>
  );
}

function NoteCell({ r }: { r: SubjectRow }) {
  const n = note(r);
  if (!n) return <td className="sj-note" />;
  return (
    <td className={`sj-note${n.warn ? " sj-note-warn" : ""}`}>
      {n.lead}
      {n.streams.map((s, i) => (
        <Fragment key={s}>
          {i > 0 ? ", " : " "}
          <RLink to={streamPath(r.account, s)} className="x mono">
            {s}
          </RLink>
        </Fragment>
      ))}
    </td>
  );
}

function Patterns({ r }: { r: SubjectRow }) {
  if (r.patterns.length === 0)
    return (
      <span className="sj-quiet">
        <span aria-hidden="true">–</span>
        <span className="sr-only">no subjects of its own</span>
      </span>
    );
  return (
    <>
      {r.patterns.map((p) => (
        <span key={p} className="sj-pattern">
          {p}
        </span>
      ))}
      {r.morePatterns > 0 && (
        <span className="sj-more">
          +{fmtInt(r.morePatterns)} more<span className="sr-only"> subjects</span>
        </span>
      )}
    </>
  );
}

/** A dash heard as why there is no rate: "no stored rate" on a row that is not a stream's. */
function NoRate({ why = "no stored rate" }: { why?: string }) {
  return (
    <span>
      <span aria-hidden="true">–</span>
      <span className="sr-only">{` ${why}`}</span>
    </span>
  );
}

/** A subscription or inbox row's delivered rate, with "2 of 3 measured" when some connections have none. */
function DeliveredCell({ r }: { r: SubjectRow }) {
  if (!hasDelivered(r)) return <NoRate why="no delivered rate" />;
  const rate = rateText(r.delivered.perSecond);
  if (rate === undefined) return <NoRate why="not measured" />;
  const part = measuredText(r.delivered, r.connections);
  return (
    <>
      {rate}
      {part && (
        <>
          {" "}
          <span className="sj-measured">{part}</span>
        </>
      )}
    </>
  );
}

/**
 * scoped: the page shows one account, so an account is not offered as a
 * link to itself. filterParam: the page's ?filter=, kept on an account's
 * link.
 */
export function SubjectsTable({ d, scoped, empty, filterParam = "" }: { d: Subjects; scoped: boolean; empty: string; filterParam?: string }) {
  const label = accountLabeller(d.rows);
  const cut = cutText(d);
  return (
    <section className="card sj-card" aria-label="Subject patterns">
      {d.rows.length === 0 ? (
        <p className="sj-foot">{empty}</p>
      ) : (
        <div className="table-wrap scroll-cue">
          <table className="tbl sj-tbl">
            <thead>
              <tr>
                <th scope="col">Pattern</th>
                <th scope="col">Account</th>
                <th scope="col">Stored in</th>
                <th scope="col">Subscribers</th>
                <th scope="col" className="num">
                  Stored msgs/s
                </th>
                <th scope="col" className="num">
                  Delivered msgs/s
                </th>
                <th scope="col">Note</th>
              </tr>
            </thead>
            <tbody>
              {d.rows.map((r) => {
                const warn = note(r)?.warn ?? false;
                const rate = rateText(r.storedPerSecond);
                const acc = <AccountName id={r.account} name={r.accountName || undefined} label={label(r.account, r.accountName || undefined)} />;
                return (
                  <tr key={rowKey(r)} className={warn ? "sj-warn" : undefined}>
                    <th scope="row">
                      <Patterns r={r} />
                    </th>
                    <td className="sj-account">
                      {scoped ? acc : <RLink to={`/subjects?account=${enc(r.account)}${filterParam ? `&filter=${enc(filterParam)}` : ""}`}>{acc}</RLink>}
                      {r.systemAccount && <span className="sj-system">system</span>}
                    </td>
                    <td className="sj-stream">{isStored(r) && <RLink to={streamPath(r.account, r.stream)}>{streamLabel(r.stream)}</RLink>}</td>
                    <td className="sj-subs">
                      <Subscribers r={r} />
                    </td>
                    <td className="num sj-rate">{!isStored(r) ? <NoRate /> : rate === undefined ? <Dash /> : rate}</td>
                    <td className="num sj-rate">
                      <DeliveredCell r={r} />
                    </td>
                    <NoteCell r={r} />
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      {cut && <p className="sj-foot">{cut}</p>}
    </section>
  );
}

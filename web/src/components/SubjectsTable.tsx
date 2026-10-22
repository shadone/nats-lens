// The Subjects page's pattern table: one row per stream (every subject of
// it in the first cell), per subscribed subject, per account's reply
// inboxes and per account's JetStream API, rows with a warning first.
import { Fragment } from "react";
import type { SubjectRow, Subjects } from "../api/subjects";
import { cutText, isStored, note, rateText, subscribers } from "../lib/subjectsView";
import { accountLabeller, streamPath } from "../lib/view";
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
  return (
    <span className="sj-names">
      {" ("}
      {names.map((n, i) => (
        <Fragment key={n}>
          {i > 0 && ", "}
          <RLink to={connectionPath(n)}>{n}</RLink>
        </Fragment>
      ))}
      {more > 0 && `${names.length ? " " : ""}+${more}`}
      {")"}
    </span>
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
          <span className="sj-nobr">{s.queue}</span>
        </>
      )}
      <Names names={s.names} more={s.more} />
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

/**
 * A subject as one span per token, each with its dot. They read as one
 * word; at phone width the stylesheet makes them inline blocks, so a
 * narrow column can break between tokens and nowhere else.
 */
function Tokens({ text }: { text: string }) {
  const parts = text.split(".");
  return (
    <>
      {parts.map((t, i) => (
        <span key={i} className="sj-tok">
          {i < parts.length - 1 ? `${t}.` : t}
        </span>
      ))}
    </>
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
          <Tokens text={p} />
        </span>
      ))}
    </>
  );
}

/** scoped: the page shows one account, so an account is not offered as a link to itself. */
/** No stored rate: the row is not a stream's, so it never has one. */
function NoRate() {
  return (
    <span>
      <span aria-hidden="true">–</span>
      <span className="sr-only">{" no stored rate"}</span>
    </span>
  );
}

/** filterParam: the page's ?filter=, kept on an account's link. */
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
                    <td className="sj-stream">{isStored(r) && <RLink to={streamPath(r.account, r.stream)}>{r.stream}</RLink>}</td>
                    <td className="sj-subs">
                      <Subscribers r={r} />
                    </td>
                    <td className="num sj-rate">{!isStored(r) ? <NoRate /> : rate === undefined ? <Dash /> : rate}</td>
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

// "Where would a message go?": the subject field beside the filters and
// the card under them with what a message on the typed subject would
// reach, by account. Only the answer for the subject in the field now is
// shown; an answer for a subject typed before never stands in.
import { Code, ConnectError } from "@connectrpc/connect";
import { Fragment } from "react";
import type { CoreMatch, StreamMatch, SubjectMatches } from "../api/subjects";
import { coreParts, invalidReason, JETSTREAM_API_ANSWERS, NOTHING_NOT_COLLECTED, NOTHING_REACHED, reachesNothing, readers } from "../lib/subjectsView";
import { upperFirst } from "../lib/format";
import { consumerPath, streamPath } from "../lib/view";
import { AccountName } from "./AccountName";
import { errorText, RLink } from "./common";
import { Names } from "./SubjectsTable";

/** The server's reason when it refused the subject, else undefined. */
export function refusal(e: unknown): string | undefined {
  return e instanceof ConnectError && e.code === Code.InvalidArgument ? invalidReason(e.rawMessage) : undefined;
}

export const WHY_ID = "sj-why";

export function SubjectField({ value, onChange, why }: { value: string; onChange: (v: string) => void; why?: string }) {
  return (
    <div className="sj-field-wrap">
      <label className={`sj-field${why ? " sj-field-bad" : ""}`}>
        <span className="mono sj-field-k" aria-hidden="true">
          subject
        </span>
        <input
          type="text"
          className="mono"
          aria-label="Test a subject"
          placeholder="orders.place"
          spellCheck={false}
          autoComplete="off"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={why ? true : undefined}
          aria-describedby={why ? WHY_ID : undefined}
        />
      </label>
      {/* Always present, so a screen reader hears a reason that appears. */}
      <p id={WHY_ID} className="sj-why" aria-live="polite">
        {why}
      </p>
    </div>
  );
}

function ConsumerNames({ s, ephemeral }: { s: StreamMatch; ephemeral: string }) {
  return (
    <>
      {s.consumers.map((c, i) => (
        <Fragment key={c}>
          {i > 0 && (i === s.consumers.length - 1 && !ephemeral ? " and " : ", ")}
          <RLink to={consumerPath(s.account, s.stream, c)} className="x mono">
            {c}
          </RLink>
        </Fragment>
      ))}
      {ephemeral && `${s.consumers.length ? " and " : ""}${ephemeral}`}
    </>
  );
}

function StreamReach({ s }: { s: StreamMatch }) {
  const r = readers(s);
  const read = s.consumers.length > 0 || r.ephemeral !== "";
  return (
    <>
      <span className="sj-chip sj-chip-stream">
        <span className="sj-k">Stream</span>{" "}
        <RLink to={streamPath(s.account, s.stream)} className="x mono">
          {s.stream}
        </RLink>{" "}
        <span className="sj-via">
          via <span className="mono">{s.pattern}</span>
        </span>
      </span>
      <span className={`sj-chip${r.warn ? " sj-chip-warn" : ""}`}>
        <span className="sj-k">{upperFirst(r.label)}</span>
        {read && (
          <>
            {" "}
            <ConsumerNames s={s} ephemeral={r.ephemeral} />
          </>
        )}
      </span>
      {s.mirrors.map((m) => (
        <span key={`m:${m}`} className="sj-chip">
          <span className="sj-k">Mirror</span>{" "}
          <RLink to={streamPath(s.account, m)} className="x mono">
            {m}
          </RLink>
        </span>
      ))}
      {s.sourcedBy.map((m) => (
        <span key={`s:${m}`} className="sj-chip">
          <span className="sj-k">Sourced by</span>{" "}
          <RLink to={streamPath(s.account, m)} className="x mono">
            {m}
          </RLink>
        </span>
      ))}
    </>
  );
}

/** A core match's lead with its queue group kept on one line. */
function CoreLead({ c }: { c: CoreMatch }) {
  const p = coreParts(c);
  return p.queue ? (
    <>
      {p.lead} <span className="sj-name">{p.queue}</span>
    </>
  ) : (
    <>{p.lead}</>
  );
}

function nothingText(m: SubjectMatches): string {
  return m.subscriptionsAt === undefined ? NOTHING_NOT_COLLECTED : NOTHING_REACHED;
}

/**
 * What the card says, from the server's groups: one per account the
 * message is published in. A heading names the account unless the page
 * shows one.
 */
function Reach({ m, scoped }: { m: SubjectMatches; scoped: boolean }) {
  if (m.accounts.length === 0) return <p className="sj-reach-none">{nothingText(m)}</p>;
  return (
    <>
      {m.accounts.map((g) => (
        <div key={g.account} className="sj-group" role="group" aria-label={`Published in ${g.accountName || g.account}`}>
          {!scoped && (
            <div className="sj-group-h">
              <span className="sj-k">Published in</span> <AccountName id={g.account} name={g.accountName || undefined} />
              {g.systemAccount && <span className="sj-system">system</span>}
            </div>
          )}
          {reachesNothing(g) ? (
            <p className="sj-reach-none">{nothingText(m)}</p>
          ) : (
            <div className="sj-chips">
              {g.streams.map((s) => (
                <StreamReach key={s.stream} s={s} />
              ))}
              {g.jetstreamApi && (
                <span className="sj-chip sj-chip-stream">{JETSTREAM_API_ANSWERS}</span>
              )}
              {g.core.length > 0 ? (
                <span className="sj-chip">
                  <span className="sj-k">Core subscribers</span>{" "}
                  {g.core.map((c, i) => (
                    <Fragment key={`${c.subject}|${c.queue}`}>
                      {i > 0 && "; "}
                      <CoreLead c={c} />
                      <Names names={c.names} more={c.moreNames} /> via <span className="mono">{c.subject}</span>
                    </Fragment>
                  ))}
                </span>
              ) : (
                <span className="sj-chip">
                  <span className="sj-k">Core subscribers</span> {m.subscriptionsAt === undefined ? "not collected" : "none"}
                </span>
              )}
            </div>
          )}
        </div>
      ))}
    </>
  );
}

/**
 * subject: the subject the answer is for. answer: undefined while it loads.
 * error: a failure other than a refused subject, which the field shows.
 */
export function ReachCard({ subject, answer, error, scoped }: { subject: string; answer?: SubjectMatches; error?: unknown; scoped: boolean }) {
  return (
    <section className="card sj-reach" aria-label="Where this subject goes">
      <div className="sj-k sj-reach-h">
        A message on <span className="mono sj-subject">{subject}</span> would reach
      </div>
      {/* One live region for the card's life, so each answer is announced. */}
      <div className="sj-reach-body" aria-live="polite">
        {error ? (
          <p className="sj-reach-none">Could not look it up: {errorText(error)}</p>
        ) : answer === undefined ? (
          <p className="sj-reach-none">Looking it up…</p>
        ) : !answer.collected ? (
          <p className="sj-reach-none">No collection in this range.</p>
        ) : (
          <Reach m={answer} scoped={scoped} />
        )}
      </div>
    </section>
  );
}
